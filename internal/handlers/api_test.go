package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ncogearthchain-api-graphql/internal/config"
	"ncogearthchain-api-graphql/internal/graphql/resolvers"
	"ncogearthchain-api-graphql/internal/logger"
	"ncogearthchain-api-graphql/internal/svc"
)

// TestApiHTTPContract exercises the API handler chain over real HTTP, end to end, WITHOUT a node or
// database: it covers exactly the paths that resolve before any repository is touched --
//
//   - the complexity guard and body-size limit (the DoS protections), on both /graphql and the
//     /graphql-ws HTTP fallback (the route that previously bypassed them);
//   - schema-level introspection, proving the delegation query surface is gone and the re-exposed
//     rewardClaims / withdrawRequests root queries are present;
//   - query validation, proving a removed field is rejected with a GraphQL error rather than a panic.
//
// A live server + node is required only to exercise the RPC-backed resolvers and real ingestion;
// this closes the HTTP-layer gap that unit tests on the individual middlewares cannot.
func newTestAPIServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	cfg := &config.Config{AppName: "explorer-test"}
	cfg.Log.Level = "CRITICAL" // keep the test output quiet
	cfg.Log.Format = "%{message}"
	cfg.Server.CorsOrigin = []string{"*"}
	cfg.Server.ResolverTimeout = 30
	cfg.Server.MaxQueryDepth = 12
	cfg.Server.MaxQueryComplexity = 25000
	cfg.Server.MaxRequestBody = 1 << 20 // 1 MiB
	cfg.Server.MaxParallelism = 10

	lg := logger.New(cfg)
	// resolvers.New() reaches svc.Manager(), which requires the svc package's config+logger
	// (the apiserver sets these in the same order at startup).
	svc.SetConfig(cfg)
	svc.SetLogger(lg)
	resolvers.SetConfig(cfg)
	resolvers.SetLogger(lg)
	rs := resolvers.New()

	ts := httptest.NewServer(Api(cfg, lg, rs))
	cleanup := func() {
		ts.Close()
		if c, ok := rs.(interface{ Close() }); ok {
			c.Close()
		}
	}
	return ts, cleanup
}

// postGraphQL sends a raw JSON body to a path and returns the status and body.
func postGraphQL(t *testing.T, url, body string) (int, []byte) {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestApiHTTPContract(t *testing.T) {
	ts, cleanup := newTestAPIServer(t)
	defer cleanup()

	t.Run("introspection: delegation removed, rewards/withdrawals re-exposed", func(t *testing.T) {
		q := `{"query":"{ __schema { queryType { fields { name } } } }"}`
		status, body := postGraphQL(t, ts.URL+"/graphql", q)
		if status != http.StatusOK {
			t.Fatalf("introspection status = %d, want 200; body=%s", status, body)
		}

		var resp struct {
			Data struct {
				Schema struct {
					QueryType struct {
						Fields []struct {
							Name string `json:"name"`
						} `json:"fields"`
					} `json:"queryType"`
				} `json:"__schema"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("introspection body not JSON: %v; body=%s", err, body)
		}
		if len(resp.Errors) != 0 {
			t.Fatalf("introspection returned errors: %+v", resp.Errors)
		}

		present := make(map[string]bool)
		for _, f := range resp.Data.Schema.QueryType.Fields {
			present[f.Name] = true
		}
		if len(present) == 0 {
			t.Fatalf("no Query fields returned; body=%s", body)
		}

		// Delegation surface must be gone.
		for _, gone := range []string{"delegation", "delegationsOf", "delegationsByAddress"} {
			if present[gone] {
				t.Errorf("removed query field %q is still in the schema", gone)
			}
		}
		// Kept + re-exposed queries must be present.
		for _, want := range []string{"transactions", "block", "rewardClaims", "withdrawRequests", "sfcRewardsCollectedAmount"} {
			if !present[want] {
				t.Errorf("expected query field %q is missing", want)
			}
		}
	})

	t.Run("complexity guard rejects an over-budget query on /graphql", func(t *testing.T) {
		// Shallow-but-wide: 500 x 500 with an internalTransactions (weight 100) leaf blows past 25000.
		q := `{"query":"{ blocks(count:500){edges{block{txList(count:500){internalTransactions{hash}}}}}}"}`
		status, body := postGraphQL(t, ts.URL+"/graphql", q)
		if status != http.StatusBadRequest {
			t.Fatalf("over-complexity status = %d, want 400 (guard should reject before execution); body=%s", status, body)
		}
	})

	t.Run("complexity guard also covers the /graphql-ws HTTP fallback", func(t *testing.T) {
		// A plain POST (no websocket upgrade) to /graphql-ws used to bypass the guard entirely.
		q := `{"query":"{ blocks(count:500){edges{block{txList(count:500){internalTransactions{hash}}}}}}"}`
		status, body := postGraphQL(t, ts.URL+"/graphql-ws", q)
		if status != http.StatusBadRequest {
			t.Fatalf("/graphql-ws over-complexity status = %d, want 400 (fallback must be guarded); body=%s", status, body)
		}
	})

	t.Run("body-size limit rejects an oversized request", func(t *testing.T) {
		// > 1 MiB of padding inside a string literal so the reader trips MaxBytesReader.
		big := strings.Repeat("A", (1<<20)+1024)
		q := `{"query":"{ block(number:\"0x1\"){hash} }","variables":{"pad":"` + big + `"}}`
		status, body := postGraphQL(t, ts.URL+"/graphql", q)
		if status != http.StatusBadRequest {
			t.Fatalf("oversized-body status = %d, want 400; body=%s", status, body)
		}
	})

	t.Run("a removed field is a GraphQL validation error, not a crash", func(t *testing.T) {
		q := `{"query":"{ delegation(address:\"0x0000000000000000000000000000000000000000\", staker:\"0x1\"){amountStaked} }"}`
		status, body := postGraphQL(t, ts.URL+"/graphql", q)
		// graphql-go validates the query against the schema before executing any resolver, so this
		// returns 200 with an errors[] entry -- no repository/node is touched.
		if status != http.StatusOK {
			t.Fatalf("removed-field status = %d, want 200 with errors; body=%s", status, body)
		}
		if !strings.Contains(string(body), "delegation") || !strings.Contains(strings.ToLower(string(body)), "cannot query field") {
			t.Fatalf("expected a 'Cannot query field \"delegation\"' validation error; body=%s", body)
		}
	})
}
