package handlers

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSanitizeGraphQLErrors pins the masking contract: a resolver error (one with a "path") is
// rewritten to the generic message, a validation error (no "path") is preserved verbatim, and a
// non-GraphQL body is returned untouched.
func TestSanitizeGraphQLErrors(t *testing.T) {
	t.Run("masks path-bearing resolver error, keeps data", func(t *testing.T) {
		in := `{"data":{"block":null},"errors":[{"message":"DDB operation query failed: ERROR: relation \"ddb_operation\" does not exist (SQLSTATE 42P01)","path":["block","txList"]}]}`
		out := sanitizeGraphQLErrors([]byte(in), nil)

		if strings.Contains(string(out), "SQLSTATE") || strings.Contains(string(out), "ddb_operation") {
			t.Fatalf("internal SQL detail leaked through the mask: %s", out)
		}
		var resp struct {
			Data   json.RawMessage `json:"data"`
			Errors []struct {
				Message string        `json:"message"`
				Path    []interface{} `json:"path"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			t.Fatalf("masked body is not valid JSON: %v", err)
		}
		if len(resp.Errors) != 1 || resp.Errors[0].Message != genericErrorMessage {
			t.Fatalf("resolver error not masked to the generic message: %+v", resp.Errors)
		}
		if len(resp.Errors[0].Path) != 2 {
			t.Fatalf("error path should be preserved, got %+v", resp.Errors[0].Path)
		}
		if string(resp.Data) != `{"block":null}` {
			t.Fatalf("data payload must survive masking, got %s", resp.Data)
		}
	})

	t.Run("preserves validation error without a path", func(t *testing.T) {
		in := `{"errors":[{"message":"Cannot query field \"nope\" on type \"Query\"."}]}`
		out := sanitizeGraphQLErrors([]byte(in), nil)
		if !strings.Contains(string(out), `Cannot query field`) {
			t.Fatalf("validation error must reach the client verbatim, got %s", out)
		}
	})

	t.Run("passes non-GraphQL body through unchanged", func(t *testing.T) {
		in := `query too complex: cost 90000 exceeds budget 25000`
		out := sanitizeGraphQLErrors([]byte(in), nil)
		if string(out) != in {
			t.Fatalf("plain-text response should be untouched, got %s", out)
		}
	})

	t.Run("no errors array is untouched", func(t *testing.T) {
		in := `{"data":{"block":{"number":"0x1"}}}`
		out := sanitizeGraphQLErrors([]byte(in), nil)
		if string(out) != in {
			t.Fatalf("error-free response should be untouched, got %s", out)
		}
	})
}
