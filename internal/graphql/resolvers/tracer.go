package resolvers

import (
	"encoding/json"
	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// TraceConfig represents the configuration for tracing.
type TraceConfig struct {
	Tracer  *string
	Timeout *string
}

// TraceBlock resolves the debug_traceBlock GraphQL query.
func (rs *rootResolver) TraceBlock(args struct{ Hash common.Hash }) (*types.TraceBlockResponse, error) {
	result, err := repository.R().TraceBlock(args.Hash)
	if err != nil {
		return nil, err
	}
	// Marshal and unmarshal to our struct
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var traceResult types.TraceBlockResult
	err = json.Unmarshal(jsonBytes, &traceResult)
	if err != nil {
		return nil, err
	}
	return &types.TraceBlockResponse{Result: traceResult}, nil
}

// TraceBlockByNumber resolves the debug_traceBlockByNumber GraphQL query.
func (rs *rootResolver) TraceBlockByNumber(args struct{ Number hexutil.Uint64 }) (*types.TraceBlockResponse, error) {
	result, err := repository.R().TraceBlockByNumber(args.Number)
	if err != nil {
		return nil, err
	}
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var traceResult types.TraceBlockResult
	err = json.Unmarshal(jsonBytes, &traceResult)
	if err != nil {
		return nil, err
	}
	return &types.TraceBlockResponse{Result: traceResult}, nil
}

// TraceBlockByHash resolves the debug_traceBlockByHash GraphQL query.
func (rs *rootResolver) TraceBlockByHash(args struct{ Hash common.Hash }) (*types.TraceBlockResponse, error) {
	result, err := repository.R().TraceBlockByHash(args.Hash)
	if err != nil {
		return nil, err
	}
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var traceResult types.TraceBlockResult
	err = json.Unmarshal(jsonBytes, &traceResult)
	if err != nil {
		return nil, err
	}
	return &types.TraceBlockResponse{Result: traceResult}, nil
}

// TraceTransaction resolves the debug_traceTransaction GraphQL query.
func (rs *rootResolver) TraceTransaction(args struct{ Hash common.Hash }) (*types.TraceBlockResponse, error) {
	result, err := repository.R().TraceTransaction(args.Hash)
	if err != nil {
		return nil, err
	}
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var traceResult types.TraceBlockResult
	err = json.Unmarshal(jsonBytes, &traceResult)
	if err != nil {
		return nil, err
	}
	return &types.TraceBlockResponse{Result: traceResult}, nil
}
