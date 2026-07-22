// Package rpc implements bridge to NCOG full node service.
package rpc

// ddb.go bridges the node's DDB (Decentralized DataBase) JSON-RPC namespace (`ddb_*`, served by
// ethapi.PublicDdbAPI) so the explorer can display on-chain relational-DB state: the validator committee,
// contract schemas, table rows, and stats. All are read-only introspection calls.

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

// DdbValidators returns the current DDB validator committee + BFT threshold (ddb_getDdbValidators).
func (br *NecBridge) DdbValidators() (interface{}, error) {
	var result interface{}
	err := br.rpc.CallContext(context.Background(), &result, "ddb_getDdbValidators")
	return result, err
}

// DdbSchema returns a contract schema's definition — tables, procedures, roles (ddb_getSchema).
func (br *NecBridge) DdbSchema(schemaName string) (interface{}, error) {
	var result interface{}
	err := br.rpc.CallContext(context.Background(), &result, "ddb_getSchema", schemaName)
	return result, err
}

// DdbSelect returns rows from a table with optional filters / ordering / pagination (ddb_select).
// A nil options is sent as an empty object so the node applies its defaults.
func (br *NecBridge) DdbSelect(schemaName, tableName string, options interface{}) (interface{}, error) {
	if options == nil {
		options = map[string]interface{}{}
	}
	var result interface{}
	err := br.rpc.CallContext(context.Background(), &result, "ddb_select", schemaName, tableName, options)
	return result, err
}

// DdbQuery returns up to limit rows from a table (ddb_query).
func (br *NecBridge) DdbQuery(schemaName, tableName string, limit int) (interface{}, error) {
	var result interface{}
	err := br.rpc.CallContext(context.Background(), &result, "ddb_query", schemaName, tableName, limit)
	return result, err
}

// DdbStats returns DDB storage stats — schema/table/operation counts (ddb_getStats).
func (br *NecBridge) DdbStats() (interface{}, error) {
	var result interface{}
	err := br.rpc.CallContext(context.Background(), &result, "ddb_getStats")
	return result, err
}

// DdbConsensusStats returns dual-consensus stats — pending/completed endorsements, threshold (ddb_getConsensusStats).
func (br *NecBridge) DdbConsensusStats() (interface{}, error) {
	var result interface{}
	err := br.rpc.CallContext(context.Background(), &result, "ddb_getConsensusStats")
	return result, err
}

// DdbEndorsementStatus returns the endorsement status for a request id (ddb_getEndorsementStatus).
func (br *NecBridge) DdbEndorsementStatus(requestID common.Hash) (interface{}, error) {
	var result interface{}
	err := br.rpc.CallContext(context.Background(), &result, "ddb_getEndorsementStatus", requestID)
	return result, err
}
