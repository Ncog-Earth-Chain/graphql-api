package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"

	"ncogearthchain-api-graphql/internal/logger"
)

// genericErrorMessage replaces an internal resolver error's text in the client-facing response.
const genericErrorMessage = "internal server error"

// maskErrors buffers the GraphQL relay response and rewrites the message of every RESOLVER error
// (one that carries a "path" to the field it failed on) to a generic string, logging the original
// server-side.
//
// The repository wraps pgx/Postgres driver errors with %w (e.g. "DDB operation query failed: %w"),
// and graph-gophers/graphql-go serializes a returned resolver error's Error() string straight into
// errors[].message. With no presenter configured on the schema, a DB-side failure (statement_timeout,
// a scan/type mismatch, a lost connection) disclosed SQLSTATE codes and table/column/constraint
// names to unauthenticated callers -- enough to fingerprint the schema and craft further probes.
//
// Validation and syntax errors carry NO path (they are produced before field resolution), so they
// pass through untouched: a client still learns it mistyped a field name or sent an invalid query.
func maskErrors(log logger.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := &responseBuffer{header: make(http.Header)}
		h.ServeHTTP(buf, r)

		body := sanitizeGraphQLErrors(buf.body.Bytes(), log)

		dst := w.Header()
		for k, vv := range buf.header {
			dst[k] = vv
		}
		dst.Set("Content-Length", strconv.Itoa(len(body)))

		status := buf.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})
}

// responseBuffer captures a handler's response so it can be inspected and rewritten before it is
// flushed to the real client. The relay handler writes a single complete JSON document, so full
// buffering is appropriate here.
type responseBuffer struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *responseBuffer) Header() http.Header { return b.header }

func (b *responseBuffer) WriteHeader(s int) {
	if b.status == 0 {
		b.status = s
	}
}

func (b *responseBuffer) Write(p []byte) (int, error) { return b.body.Write(p) }

// sanitizeGraphQLErrors replaces the message of each path-bearing error with genericErrorMessage,
// logging the original. It returns the input unchanged when the body is not a JSON object carrying
// an errors array -- so a plain-text 400 from the complexity guard, or any non-GraphQL response,
// passes straight through.
func sanitizeGraphQLErrors(body []byte, log logger.Logger) []byte {
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(body, &resp); err != nil {
		return body
	}
	rawErrs, ok := resp["errors"]
	if !ok {
		return body
	}

	var errs []map[string]json.RawMessage
	if err := json.Unmarshal(rawErrs, &errs); err != nil {
		return body
	}

	changed := false
	for _, e := range errs {
		if _, hasPath := e["path"]; !hasPath {
			// No path -> a validation/syntax error the client needs to see verbatim.
			continue
		}
		var msg string
		_ = json.Unmarshal(e["message"], &msg)
		if msg == genericErrorMessage {
			continue
		}
		if log != nil {
			log.Errorf("masked GraphQL resolver error: %s", msg)
		}
		masked, err := json.Marshal(genericErrorMessage)
		if err != nil {
			continue
		}
		e["message"] = masked
		changed = true
	}

	if !changed {
		return body
	}

	newErrs, err := json.Marshal(errs)
	if err != nil {
		return body
	}
	resp["errors"] = newErrs

	out, err := json.Marshal(resp)
	if err != nil {
		return body
	}
	return out
}
