package types

import "errors"

// The four ways DdbOperationTarget can fail to name the contract an operation belongs to. Values
// rather than formatted strings so a caller can tell them apart without matching text, and
// deliberately terse: the surface that reports them (the withheld descriptor) names the reason the
// node would have given, not the parse detail.
var (
	errDdbNoSchemaNamed         = errors.New("the DDB operation names no schema")
	errDdbOperationUnparseable  = errors.New("the DDB operation JSON does not parse")
	errDdbDefinitionUnparseable = errors.New("the contract definition the DDB operation carries " +
		"does not parse")
	errDdbNoContractAddress = errors.New("the contract definition the DDB operation carries names " +
		"no contract address a schema can be derived from")
)
