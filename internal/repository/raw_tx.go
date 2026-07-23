package repository

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// RawTransaction fetches a transaction's canonical RLP encoding from the node.
//
// This is what makes storing ML-DSA public keys unnecessary. The encoding carries the
// signature and public key, and it is verifiable against a hash this explorer already
// holds, so attribution stays provable at zero storage cost. See TransactionSigner.
//
// Requires a node with TxIndex enabled; an empty result means "cannot answer", which the
// caller renders as NULL rather than as a failure.
func (p *proxy) RawTransaction(ctx context.Context, hash *common.Hash) ([]byte, error) {
	if hash == nil {
		return nil, fmt.Errorf("no transaction hash given")
	}
	return p.rpc.RawTransaction(ctx, hash)
}

// DecodeRawTransaction extracts the signature, public key and claimed sender from a raw
// transaction.
//
// It decodes with the LOCAL types package, which is the post-wire-break shape: LegacyTx
// carries Signature, PubKey, ChainID, From and SigVer. A blob that does not decode
// against it is reported as an error rather than being partially interpreted -- the RLP
// decoder is strict again (see ncog-evm rlp/canonical_test.go), so a truncated or
// trailing-garbage encoding fails here instead of silently zero-filling the fields that
// carry the attribution.
func (p *proxy) DecodeRawTransaction(ctx context.Context, raw []byte) (sig []byte, pub []byte, from common.Address, err error) {
	var tx types.Transaction
	if err = rlp.DecodeBytes(raw, &tx); err != nil {
		return nil, nil, common.Address{}, fmt.Errorf("can not decode the raw transaction: %w", err)
	}

	// ClaimedFrom, not a recovered sender: Model B carries the stable address on the
	// transaction, and recovering it is impossible here anyway. Naming it "claimed" keeps
	// the distinction visible -- the resolver's job is to check it against the key.
	sig = tx.RawSignatureValues()
	pub = tx.PublicKeyValue()
	from = tx.ClaimedFrom()
	return sig, pub, from, nil
}
