package resolvers

// ddb.go resolves the DDB (Decentralized DataBase) GraphQL queries. Each returns JSONAny wrapping the
// node's ddb_* RPC result — schemas, rows and stats are user-defined / open-shaped, so a rigid GraphQL
// type would be brittle; the explorer parses the JSON client-side (same approach as the trace queries).

import (
	"ncogearthchain-api-graphql/internal/repository"

	"github.com/ethereum/go-ethereum/common"
)

// DdbValidators resolves the ddbValidators query — the DDB committee + threshold.
func (rs *rootResolver) DdbValidators() (JSONAny, error) {
	res, err := repository.R().DdbValidators()
	return JSONAny{Value: res}, err
}

// DdbSchema resolves the ddbSchema query — a contract schema's definition.
func (rs *rootResolver) DdbSchema(args struct{ SchemaName string }) (JSONAny, error) {
	res, err := repository.R().DdbSchema(args.SchemaName)
	return JSONAny{Value: res}, err
}

// DdbSelect resolves the ddbSelect query — rows with optional filters / ordering / pagination.
func (rs *rootResolver) DdbSelect(args struct {
	SchemaName string
	TableName  string
	Options    *JSONAny
}) (JSONAny, error) {
	var opts interface{}
	if args.Options != nil {
		opts = args.Options.Value
	}
	res, err := repository.R().DdbSelect(args.SchemaName, args.TableName, opts)
	return JSONAny{Value: res}, err
}

// DdbQuery resolves the ddbQuery query — up to limit rows from a table.
func (rs *rootResolver) DdbQuery(args struct {
	SchemaName string
	TableName  string
	Limit      int32
}) (JSONAny, error) {
	res, err := repository.R().DdbQuery(args.SchemaName, args.TableName, int(args.Limit))
	return JSONAny{Value: res}, err
}

// DdbStats resolves the ddbStats query — DDB storage stats.
func (rs *rootResolver) DdbStats() (JSONAny, error) {
	res, err := repository.R().DdbStats()
	return JSONAny{Value: res}, err
}

// DdbConsensusStats resolves the ddbConsensusStats query — dual-consensus stats.
func (rs *rootResolver) DdbConsensusStats() (JSONAny, error) {
	res, err := repository.R().DdbConsensusStats()
	return JSONAny{Value: res}, err
}

// DdbEndorsementStatus resolves the ddbEndorsementStatus query for a request id.
func (rs *rootResolver) DdbEndorsementStatus(args struct{ RequestId common.Hash }) (JSONAny, error) {
	res, err := repository.R().DdbEndorsementStatus(args.RequestId)
	return JSONAny{Value: res}, err
}
