// Package handlers holds HTTP/WS handlers chain along with separate middleware implementations.
package handlers

import (
	"context"
	"encoding/json"
	"ncogearthchain-api-graphql/internal/logger"
	"ncogearthchain-api-graphql/internal/repository"
	"net/http"
	"time"
)

// healthTimeout bounds the readiness check so a stalled database cannot hold the probe open.
const healthTimeout = 3 * time.Second

// Health constructs the liveness/readiness probe handler.
//
// The API is healthy when its PostgreSQL backend is reachable and the ingest watermark is
// queryable -- which is what a load balancer should cut over on. Before this, the only signal
// available was a real GraphQL query, so a probe either paid full query cost or could not
// distinguish "backend down" from "query slow". Responds 200 with the ingest head, or 503
// when the backend is unavailable.
func Health(log logger.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
		defer cancel()

		head, err := repository.R().Healthy(ctx)

		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			log.Warningf("health check failed; %s", err.Error())
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "unavailable"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":         "ok",
			"contiguousHead": head,
		})
	})
}
