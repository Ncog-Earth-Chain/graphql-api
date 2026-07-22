package resolvers

import (
	"fmt"

	"ncogearthchain-api-graphql/internal/repository"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Tracing re-executes transactions on the node, so it is by far the most
// expensive thing this API can ask for. Everything below is therefore
// allow-listed rather than forwarded.
//
// The previous implementation passed a client-supplied map straight through to
// the node's TraceConfig, which accepts:
//
//	Tracer  *string  -- an arbitrary JavaScript tracer, executed by the node
//	Reexec  *uint64  -- how many ancestor blocks to re-execute to rebuild state
//	Timeout *string  -- how long the node may spend on it
//
// An anonymous caller could therefore run their own code on the node, force
// re-execution of an unbounded number of ancestor blocks, and raise their own
// time budget while doing it.

// allowedTracers are the named, node-provided tracers a client may select.
// A raw JavaScript tracer is never accepted, no matter how it is spelled.
var allowedTracers = map[string]bool{
	"callTracer":     true,
	"prestateTracer": true,
	"4byteTracer":    true,
	"noopTracer":     true,
}

// traceStructLogKeys are the boolean struct-logger switches that are safe to
// expose: they only ever REDUCE the volume of work and output.
var traceStructLogKeys = map[string]bool{
	"disableStorage": true,
	"disableMemory":  true,
	"disableStack":   true,
}

// sanitizeTraceParams reduces client-supplied trace parameters to the safe
// subset. Unknown keys are rejected rather than dropped, so a caller who thinks
// they set a limit is never silently ignored.
func sanitizeTraceParams(in *JSONAny) (map[string]interface{}, error) {
	if in == nil {
		// no options: the node applies its own defaults
		return nil, nil
	}

	raw, ok := in.Value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("params must be an object")
	}

	out := make(map[string]interface{}, len(raw))
	for k, v := range raw {
		switch {
		case k == "tracer":
			name, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("tracer must be a string naming a built-in tracer")
			}
			if !allowedTracers[name] {
				return nil, fmt.Errorf(
					"tracer %q is not permitted; allowed tracers are callTracer, prestateTracer, 4byteTracer, noopTracer",
					name)
			}
			out[k] = name

		case traceStructLogKeys[k]:
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("%s must be a boolean", k)
			}
			out[k] = b

		case k == "reexec" || k == "timeout":
			// Deliberately not forwarded. Both let a caller enlarge the node's
			// workload for a single request; they belong to the operator.
			return nil, fmt.Errorf("%q is not a client-settable trace option", k)

		default:
			return nil, fmt.Errorf("unknown trace option %q", k)
		}
	}

	return out, nil
}

func (rs *rootResolver) TraceBlockByNumber(args struct {
	Number hexutil.Uint64
	Params *JSONAny
}) (JSONAny, error) {
	params, err := sanitizeTraceParams(args.Params)
	if err != nil {
		return JSONAny{}, err
	}

	result, err := repository.R().TraceBlockByNumber(args.Number, params)
	if err != nil {
		return JSONAny{}, err
	}
	return JSONAny{Value: result}, nil
}

func (rs *rootResolver) TraceBlockByHash(args struct {
	Hash   common.Hash
	Params *JSONAny
}) (JSONAny, error) {
	params, err := sanitizeTraceParams(args.Params)
	if err != nil {
		return JSONAny{}, err
	}

	result, err := repository.R().TraceBlockByHash(args.Hash, params)
	if err != nil {
		return JSONAny{}, err
	}
	return JSONAny{Value: result}, nil
}

func (rs *rootResolver) TraceTransaction(args struct {
	Hash   common.Hash
	Params *JSONAny
}) (JSONAny, error) {
	params, err := sanitizeTraceParams(args.Params)
	if err != nil {
		return JSONAny{}, err
	}

	result, err := repository.R().TraceTransaction(args.Hash, params)
	if err != nil {
		return JSONAny{}, err
	}
	return JSONAny{Value: result}, nil
}
