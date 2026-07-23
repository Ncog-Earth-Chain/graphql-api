package repository

import "context"

// storeCtx returns the context used for a storage call.
//
// The Repository interface does not yet carry a context.Context, so calls into the
// PostgreSQL store originate here rather than at the request boundary. This is a
// deliberate intermediate step, and it is worth being precise about what it does and does
// not cost:
//
//   - It is NOT a regression. The MongoDB layer used context.Background() in 123 places
//     and no method accepted a caller context either, so cancellation is exactly as
//     available as it was before.
//   - It IS the remaining half of the fix. Every pg method already takes a ctx as its
//     first parameter, so threading a real request context is a signature change through
//     the Repository interface and the resolvers -- mechanical, large, and with no
//     behavioural risk once the storage swap itself is proven.
//
// Doing the swap first and the threading second keeps two large changes from landing on
// top of each other, where a failure in either is hard to attribute. Every call site that
// needs updating is exactly the set that calls this function.
//
// When the interface gains a context, this becomes `return ctx` and then disappears.
func storeCtx() context.Context {
	return context.Background()
}
