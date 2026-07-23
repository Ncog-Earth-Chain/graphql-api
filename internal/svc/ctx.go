package svc

import (
	"context"
	"sync"
)

// Lifecycle context for the ingest pipeline.
//
// The services in this package are background workers, not request handlers, so there is
// no caller context to inherit. What they do have is a lifetime, and binding storage
// calls to it is strictly better than context.Background(): on shutdown, in-flight
// queries are cancelled rather than left running against a database the process is about
// to disconnect from.
//
// This is deliberately package-level rather than threaded through every dispatcher. The
// pipeline has exactly one lifetime -- the manager's -- so a per-dispatcher context would
// carry no additional information while touching every constructor.

var (
	svcCtxOnce   sync.Once
	svcCtx       context.Context
	svcCtxCancel context.CancelFunc
)

// bgCtx returns the pipeline's lifecycle context.
//
// Cancelled by stopCtx() when the service manager shuts down.
func bgCtx() context.Context {
	svcCtxOnce.Do(func() {
		svcCtx, svcCtxCancel = context.WithCancel(context.Background())
	})
	return svcCtx
}

// stopCtx cancels the pipeline's lifecycle context, unblocking any storage call still in
// flight so the workers can exit instead of waiting on a query nobody will read.
func stopCtx() {
	bgCtx()
	svcCtxCancel()
}
