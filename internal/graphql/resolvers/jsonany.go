// resolvers/jsonany.go

package resolvers

import (
	"encoding/json"
	"fmt"
	"io"
)

type JSONAny struct {
	Value interface{}
}

// 1) Tell graphql-go that *JSONAny backs the scalar named "JSONAny" in your schema:
func (*JSONAny) ImplementsGraphQLType(name string) bool {
	return name == "JSONAny"
}

// 2) Decode any input JSON into *JSONAny:
func (j *JSONAny) UnmarshalGraphQL(input interface{}) error {
	switch v := input.(type) {
	case string:
		// GraphiQL will only let you send scalars, so it's a JSON‐encoded string
		var obj interface{}
		if err := json.Unmarshal([]byte(v), &obj); err != nil {
			return fmt.Errorf("could not parse JSONAny string: %w", err)
		}
		j.Value = obj
	case map[string]interface{}:
		// e.g. if a client sends a real JSON object
		j.Value = v
	default:
		// numbers, booleans, etc.
		j.Value = v
	}
	return nil
}

// 3) Serialize JSONAny back to the client:
func (j JSONAny) MarshalGQL(w io.Writer) {
	b, err := json.Marshal(j.Value)
	if err != nil {
		w.Write([]byte("null"))
		return
	}
	w.Write(b)
}
