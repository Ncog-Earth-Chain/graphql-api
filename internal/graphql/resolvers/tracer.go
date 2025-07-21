package resolvers

import (
	"ncogearthchain-api-graphql/internal/repository"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// TraceConfig represents the configuration for tracing.
type TraceConfig struct {
	Tracer  *string
	Timeout *string
}

// TraceBlock resolves the debug_traceBlock GraphQL query.
func (rs *rootResolver) TraceBlock(args struct{ Hash common.Hash }) (interface{}, error) {
	return repository.R().TraceBlock(args.Hash)
}

// TraceBlockFromFile resolves the debug_traceBlockFromFile GraphQL query.
func (rs *rootResolver) TraceBlockFromFile(args struct{ FileName string }) (interface{}, error) {
	return repository.R().TraceBlockFromFile(args.FileName)
}

// TraceBadBlock resolves the debug_traceBadBlock GraphQL query.
func (rs *rootResolver) TraceBadBlock(args struct{ Hash common.Hash }) (interface{}, error) {
	return repository.R().TraceBadBlock(args.Hash)
}

// StandardTraceBadBlockToFile resolves the debug_standardTraceBadBlockToFile GraphQL query.
func (rs *rootResolver) StandardTraceBadBlockToFile(args struct {
	Hash     common.Hash
	FileName string
}) (interface{}, error) {
	return repository.R().StandardTraceBadBlockToFile(args.Hash, args.FileName)
}

// StandardTraceBlockToFile resolves the debug_standardTraceBlockToFile GraphQL query.
func (rs *rootResolver) StandardTraceBlockToFile(args struct {
	Hash     common.Hash
	FileName string
}) (interface{}, error) {
	return repository.R().StandardTraceBlockToFile(args.Hash, args.FileName)
}

// TraceBlockByNumber resolves the debug_traceBlockByNumber GraphQL query.
func (rs *rootResolver) TraceBlockByNumber(args struct{ Number hexutil.Uint64 }) (interface{}, error) {
	return repository.R().TraceBlockByNumber(args.Number)
}

// TraceBlockByHash resolves the debug_traceBlockByHash GraphQL query.
func (rs *rootResolver) TraceBlockByHash(args struct{ Hash common.Hash }) (interface{}, error) {
	return repository.R().TraceBlockByHash(args.Hash)
}

// TraceTransaction resolves the debug_traceTransaction GraphQL query.
func (rs *rootResolver) TraceTransaction(args struct{ Hash common.Hash }) (interface{}, error) {
	return repository.R().TraceTransaction(args.Hash)
}

// TraceCall resolves the debug_traceCall GraphQL query.
func (rs *rootResolver) TraceCall(args struct {
	Call  map[string]interface{}
	Block hexutil.Uint64
}) (interface{}, error) {
	return repository.R().TraceCall(args.Call, args.Block)
}
