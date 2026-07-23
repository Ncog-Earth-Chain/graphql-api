package resolvers

import (
	"context"

	"ncogearthchain-api-graphql/internal/repository"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// TransactionSigner resolves the cryptographically verified attribution of a transaction.
//
// This is the answer to a problem the post-quantum switch created and that an ECDSA
// explorer never has to solve. With secp256k1, `from` is derivable from the signature by
// anyone, so showing it is showing a proof. ML-DSA has NO KEY RECOVERY: the public key is
// a separate ~2592-byte artefact, and without it `from` is only what the transaction
// CLAIMS about itself.
//
// Storing public keys would make attribution provable from the database, but at roughly
// 70% of total storage (see the schema note in 00002_block_tx.sql) that is the wrong
// trade. Instead the credentials are fetched from the node on demand -- and the fetch is
// SELF-VERIFYING, which is what makes trusting the node unnecessary:
//
//	keccak256(rawTransaction) == transactionHash
//
// holds exactly, because the transaction hash IS the RLP hash of the same inner structure
// that carries the signature and public key. A node returning a different transaction's
// credentials, or fabricated ones, fails that check against a hash this explorer already
// holds and trusts. One keccak, no storage.
type TransactionSigner struct {
	pubKey    hexutil.Bytes
	signature hexutil.Bytes
	claimed   common.Address
	derived   common.Address
	verified  bool
}

// Signer resolves the attribution of a transaction.
//
// Returns nil when the node cannot supply the raw transaction, which needs a full-history
// node with TxIndex enabled. Nil means "cannot prove", never "proof failed" -- those are
// different answers and collapsing them would let an unreachable archive read as a bad
// signature.
func (trx *Transaction) Signer(ctx context.Context) (*TransactionSigner, error) {
	raw, err := repository.R().RawTransaction(ctx, &trx.Hash)
	if err != nil || len(raw) == 0 {
		// Not an error to the client: an explorer pointed at a pruned node simply cannot
		// answer this, and saying so with NULL is more honest than a failed query.
		return nil, nil
	}

	// THE CHECK. Everything below is only meaningful if this passes.
	verified := crypto.Keccak256Hash(raw) == trx.Hash

	sig, pub, from, err := repository.R().DecodeRawTransaction(ctx, raw)
	if err != nil {
		return nil, nil
	}

	// The address this key produces. Under key rotation it legitimately differs from the
	// account address, which is exactly the divergence this field exists to expose.
	var derived common.Address
	if len(pub) > 0 {
		derived = common.BytesToAddress(crypto.Keccak256(pub)[12:])
	}

	return &TransactionSigner{
		pubKey:    pub,
		signature: sig,
		claimed:   from,
		derived:   derived,
		verified:  verified,
	}, nil
}

// PubKey resolves the signer's raw ML-DSA-87 public key.
func (s *TransactionSigner) PubKey() hexutil.Bytes { return s.pubKey }

// Signature resolves the ML-DSA-87 signature.
func (s *TransactionSigner) Signature() hexutil.Bytes { return s.signature }

// DerivedAddress resolves keccak256(pubKey)[12:].
func (s *TransactionSigner) DerivedAddress() common.Address { return s.derived }

// ClaimedAddress resolves the `from` the transaction carries.
func (s *TransactionSigner) ClaimedAddress() common.Address { return s.claimed }

// Matches reports whether the key's address equals the claimed address.
//
// False is not proof of fraud -- a rotated account (sigVersion 3) keeps its address while
// its signing key changes, so the two legitimately diverge. It is the signal that they
// HAVE diverged, which a client shown only `from` could never see.
func (s *TransactionSigner) Matches() bool { return s.derived == s.claimed }

// Verified reports whether keccak256(rawTransaction) == transactionHash.
//
// If false, the node returned something that is not this transaction and no other field
// here should be believed.
func (s *TransactionSigner) Verified() bool { return s.verified }

// SigVersion resolves the signature-scheme version as a GraphQL Int.
//
// The domain type stores it as *hexutil.Uint64, which graphql-go cannot bind to Int -- a
// hex-string-marshalling type against a JSON number. The version is a small enum (2 or 3),
// so int32 is the honest wire shape; using the Long/hex scalar would render "0x2", which
// is a strange way to spell a version number.
func (trx *Transaction) SigVersion() *int32 {
	if trx.Transaction.SigVersion == nil {
		return nil
	}
	v := int32(*trx.Transaction.SigVersion)
	return &v
}

// ChainID resolves the chain this transaction was signed for.
//
// Named ChainID rather than relying on field promotion because the domain field is
// *hexutil.Big and the schema declares BigInt; the explicit method keeps the mapping
// visible next to SigVersion rather than depending on a lucky name match.
func (trx *Transaction) ChainId() *hexutil.Big {
	return trx.Transaction.ChainID
}
