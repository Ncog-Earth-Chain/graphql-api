package resolvers

import (
	"testing"

	gqlschema "ncogearthchain-api-graphql/internal/graphql/schema"

	graphql "github.com/graph-gophers/graphql-go"
)

// TestSchemaBindsDdbResolvers replicates the production MustParseSchema call (handlers.Api uses the same
// UseFieldResolvers option) to prove every schema Query field — including the new ddb* queries — binds to
// a resolver method. graphql-go panics at parse time if any field lacks a matching resolver, so a
// mismatched ddb method name/signature would fail here. Reflecting on a bare *rootResolver is enough:
// MustParseSchema only inspects the type's methods, not a live repository.
func TestSchemaBindsDdbResolvers(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("schema failed to bind resolvers (a ddb query field likely lacks a matching method): %v", r)
		}
	}()
	_ = graphql.MustParseSchema(gqlschema.Schema(), &rootResolver{}, graphql.UseFieldResolvers())
}
