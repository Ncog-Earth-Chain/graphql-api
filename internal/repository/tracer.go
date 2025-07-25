package repository

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Tracer RPC methods
func (p *proxy) TraceBlock(hash common.Hash, params map[string]interface{}) (interface{}, error) {
	return p.rpc.TraceBlock(hash, params)
}

func (p *proxy) TraceBlockByNumber(number hexutil.Uint64, params map[string]interface{}) (interface{}, error) {
	return p.rpc.TraceBlockByNumber(number, params)
}

func (p *proxy) TraceBlockByHash(hash common.Hash, params map[string]interface{}) (interface{}, error) {
	return p.rpc.TraceBlockByHash(hash, params)
}

func (p *proxy) TraceTransaction(hash common.Hash, params map[string]interface{}) (interface{}, error) {
	return p.rpc.TraceTransaction(hash, params)
}
