// Package handlers holds HTTP/WS handlers chain along with separate middleware implementations.
package handlers

import (
	"ncogearthchain-api-graphql/internal/config"
	"ncogearthchain-api-graphql/internal/graphql/resolvers"
	gqlSchema "ncogearthchain-api-graphql/internal/graphql/schema"
	"ncogearthchain-api-graphql/internal/logger"
	"net/http"

	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
	"github.com/graph-gophers/graphql-transport-ws/graphqlws"
	"github.com/rs/cors"
)

// Api constructs and return the API HTTP handlers chain for serving GraphQL API calls.
func Api(cfg *config.Config, log logger.Logger, rs resolvers.ApiResolver) http.Handler {
	// Create new CORS handler and attach the logger into it so we get information on Debug level if needed
	corsHandler := cors.New(corsOptions(cfg))
	corsHandler.Log = log

	// we don't want to write a method for each type field if it could be matched directly
	opts := []graphql.SchemaOpt{
		graphql.UseFieldResolvers(),

		// The schema is cyclic: Block.txList -> Transaction.block -> txList, and
		// Block.parent -> Block resolves one RPC call per level. Without a depth
		// bound a single anonymous query multiplies node work without limit.
		graphql.MaxDepth(cfg.Server.MaxQueryDepth),

		// bound concurrent resolver goroutines per query
		graphql.MaxParallelism(cfg.Server.MaxParallelism),
	}

	// create new parsed GraphQL schema
	schema := graphql.MustParseSchema(gqlSchema.Schema(), rs, opts...)

	// Depth alone does not stop a shallow-but-wide query, so estimate the cost of
	// the query before executing it and reject the expensive ones.
	guard := NewComplexityGuard(cfg.Server.MaxQueryComplexity, log)

	// bound the accepted request body; queries are text, large bodies are abuse
	limitBody := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, cfg.Server.MaxRequestBody)
			h.ServeHTTP(w, r)
		})
	}

	// return the constructed API handler chain
	// Mux to handle separate endpoints cleanly
	mux := http.NewServeMux()

	// HTTP for queries/mutations.
	//
	// Both paths are registered here because this mux is itself mounted behind the
	// server's outer mux on /api AND /graphql. Registering only /graphql meant a
	// request to /api reached this inner mux with path "/api", matched nothing, and
	// 404'd -- even though /api is the endpoint the API advertises to peers.
	queryHandler := corsHandler.Handler(limitBody(guard.Handler(maskErrors(log, &relay.Handler{Schema: schema}))))
	mux.Handle("/graphql", queryHandler)
	mux.Handle("/api", queryHandler)

	// WS for subscriptions.
	//
	// graphqlws.NewHandlerFunc only upgrades requests that carry the "graphql-ws" websocket
	// subprotocol; anything else (e.g. a plain HTTP POST with no Sec-WebSocket-Protocol header)
	// falls through to the httpHandler argument. If that fallback is the bare relay.Handler, a
	// POST to /graphql-ws executes the full schema with NONE of the DoS protections the /graphql
	// and /api routes enforce -- no complexity budget and no body cap -- so a single shallow-but-
	// wide query can amplify backend work and an oversized body can exhaust memory. Wrap the
	// fallback with the same limitBody+guard chain so every path the schema is reachable from is
	// guarded. (A resolver TimeoutHandler is intentionally NOT added here: it would also cut off
	// legitimate long-lived websocket subscriptions.)
	mux.Handle("/graphql-ws", corsHandler.Handler(graphqlws.NewHandlerFunc(schema, limitBody(guard.Handler(maskErrors(log, &relay.Handler{Schema: schema}))))))

	// Return wrapped handler with logging
	return &LoggingHandler{
		logger:  log,
		handler: mux,
	}
}

// corsOptions constructs new set of options for the CORS handler based on provided configuration.
func corsOptions(cfg *config.Config) cors.Options {
	return cors.Options{
		AllowedOrigins: cfg.Server.CorsOrigin,
		AllowedMethods: []string{"HEAD", "GET", "POST"},
		AllowedHeaders: []string{"Origin", "Accept", "Content-Type", "X-Requested-With"},
		MaxAge:         300,
	}
}
