package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// traceBlockView carries only the identity and membership needed to authorize a
// raw block trace. Pointers distinguish absent/null fields from a real genesis
// identity or an empty block. An unknown membership cannot authorize tracing.
type traceBlockView struct {
	Hash         *common.Hash    `json:"hash"`
	Number       *hexutil.Uint64 `json:"number"`
	Transactions *[]common.Hash  `json:"transactions"`
}

func (p *proxy) traceBlockForPolicy(ctx context.Context, method string, selector interface{}) (*traceBlockView, error) {
	var block *traceBlockView
	if err := p.rpc.Connection().CallContext(ctx, &block, method, selector, false); err != nil {
		return nil, fmt.Errorf("cannot determine trace block: %w", err)
	}
	if block == nil || block.Hash == nil || block.Number == nil || block.Transactions == nil {
		return nil, errors.New("cannot determine trace block identity and transactions")
	}
	return block, nil
}

// requireTraceReadable asks the existing node policy about EVERY transaction,
// before requesting debug output. Raw traces have tracer-dependent shapes (and
// may include memory/stack data), so removing just a JSON input field would not
// close the surface. Refuse the entire trace if any verdict is unavailable or
// withheld. These expensive requests take a fresh verdict, not the index's
// short-lived cross-request cache; ordinary/public transactions remain allowed.
func (p *proxy) requireTraceReadable(ctx context.Context, hashes []common.Hash) error {
	for _, hash := range hashes {
		if hash == (common.Hash{}) {
			return errors.New("cannot determine trace transaction identity")
		}
	}
	verdicts, err := p.rpc.DdbCommitVerdicts(ctx, hashes)
	if err != nil {
		return fmt.Errorf("cannot determine trace read policy: %w", err)
	}
	for _, hash := range hashes {
		verdict, found := verdicts[hash]
		if !found || verdict.Withheld != nil {
			return errors.New("trace refused: transaction calldata is withheld or unavailable")
		}
	}
	return nil
}
