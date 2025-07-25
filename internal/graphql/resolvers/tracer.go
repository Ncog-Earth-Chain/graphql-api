package resolvers

import (
	"fmt"

	"ncogearthchain-api-graphql/internal/repository"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

func (rs *rootResolver) TraceBlock(args struct {
	Hash   common.Hash
	Params *JSONAny // ← accept JSONAny as input
}) (JSONAny, error) { // ← return JSONAny (not *JSONAny)
	var params map[string]interface{}
	if args.Params != nil {
		m, ok := args.Params.Value.(map[string]interface{})
		if !ok {
			return JSONAny{}, fmt.Errorf("params must be an object")
		}
		params = m
	}

	result, err := repository.R().TraceBlock(args.Hash, params)
	if err != nil {
		return JSONAny{}, err
	}
	// wrap your raw result (which is probably a map[string]interface{})
	// back into JSONAny
	return JSONAny{Value: result}, nil
}

func (rs *rootResolver) TraceBlockByNumber(args struct {
	Number hexutil.Uint64
	Params *JSONAny
}) (JSONAny, error) {
	var params map[string]interface{}
	if args.Params != nil {
		m, ok := args.Params.Value.(map[string]interface{})
		if !ok {
			return JSONAny{}, fmt.Errorf("params must be an object")
		}
		params = m
	}
	result, err := repository.R().TraceBlockByNumber(args.Number, params)
	if err != nil {
		return JSONAny{}, err
	}
	return JSONAny{Value: result}, nil
}

// TraceBlockByHash is identical to TraceBlock (kept for symmetry).
func (rs *rootResolver) TraceBlockByHash(args struct {
	Hash   common.Hash
	Params *JSONAny
}) (JSONAny, error) {
	var params map[string]interface{}
	if args.Params != nil {
		m, ok := args.Params.Value.(map[string]interface{})
		if !ok {
			return JSONAny{}, fmt.Errorf("params must be an object")
		}
		params = m
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
	var params map[string]interface{}
	if args.Params != nil {
		m, ok := args.Params.Value.(map[string]interface{})
		if !ok {
			return JSONAny{}, fmt.Errorf("params must be an object")
		}
		params = m
	}
	result, err := repository.R().TraceTransaction(args.Hash, params)
	if err != nil {
		return JSONAny{}, err
	}
	return JSONAny{Value: result}, nil
}
