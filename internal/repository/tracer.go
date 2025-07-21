package repository

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Tracer RPC methods
func (p *proxy) TraceBlock(hash common.Hash) (interface{}, error) {
	return p.rpc.TraceBlock(hash)
}

func (p *proxy) TraceBlockFromFile(fileName string) (interface{}, error) {
	return p.rpc.TraceBlockFromFile(fileName)
}

func (p *proxy) TraceBadBlock(hash common.Hash) (interface{}, error) {
	return p.rpc.TraceBadBlock(hash)
}

func (p *proxy) StandardTraceBadBlockToFile(hash common.Hash, fileName string) (interface{}, error) {
	return p.rpc.StandardTraceBadBlockToFile(hash, fileName)
}

func (p *proxy) StandardTraceBlockToFile(hash common.Hash, fileName string) (interface{}, error) {
	return p.rpc.StandardTraceBlockToFile(hash, fileName)
}

func (p *proxy) TraceBlockByNumber(number hexutil.Uint64) (interface{}, error) {
	return p.rpc.TraceBlockByNumber(number)
}

func (p *proxy) TraceBlockByHash(hash common.Hash) (interface{}, error) {
	return p.rpc.TraceBlockByHash(hash)
}

func (p *proxy) TraceTransaction(hash common.Hash) (interface{}, error) {
	return p.rpc.TraceTransaction(hash)
}

func (p *proxy) TraceCall(call map[string]interface{}, block hexutil.Uint64) (interface{}, error) {
	return p.rpc.TraceCall(call, block)
}
