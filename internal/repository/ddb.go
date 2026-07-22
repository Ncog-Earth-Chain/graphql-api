package repository

import "github.com/ethereum/go-ethereum/common"

// ddb.go exposes the node's DDB (Decentralized DataBase) introspection to the GraphQL layer by delegating
// to the RPC bridge. Read-only; no local cache (DDB state changes on finality, so callers read live).

// DdbValidators returns the current DDB validator committee + threshold.
func (p *proxy) DdbValidators() (interface{}, error) { return p.rpc.DdbValidators() }

// DdbSchema returns a contract schema's definition.
func (p *proxy) DdbSchema(schemaName string) (interface{}, error) { return p.rpc.DdbSchema(schemaName) }

// DdbSelect returns rows from a table with optional filters / ordering / pagination.
func (p *proxy) DdbSelect(schemaName, tableName string, options interface{}) (interface{}, error) {
	return p.rpc.DdbSelect(schemaName, tableName, options)
}

// DdbQuery returns up to limit rows from a table.
func (p *proxy) DdbQuery(schemaName, tableName string, limit int) (interface{}, error) {
	return p.rpc.DdbQuery(schemaName, tableName, limit)
}

// DdbStats returns DDB storage stats.
func (p *proxy) DdbStats() (interface{}, error) { return p.rpc.DdbStats() }

// DdbConsensusStats returns dual-consensus stats.
func (p *proxy) DdbConsensusStats() (interface{}, error) { return p.rpc.DdbConsensusStats() }

// DdbEndorsementStatus returns the endorsement status for a request id.
func (p *proxy) DdbEndorsementStatus(requestID common.Hash) (interface{}, error) {
	return p.rpc.DdbEndorsementStatus(requestID)
}
