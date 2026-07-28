// Package resolvers implements GraphQL resolvers to incoming API requests.
package resolvers

import (
	"context"
	"math/big"
	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"golang.org/x/sync/singleflight"
)

// accMaxTransactionsPerRequest maximal number of transaction end-client can request in one query.
const accMaxTransactionsPerRequest = 250

// Account represents resolvable blockchain account structure.
type Account struct {
	types.Account
	cg singleflight.Group
}

// NewAccount builds new resolvable account structure.
func NewAccount(acc *types.Account) *Account {
	return &Account{
		Account: *acc,
	}
}

// Account resolves blockchain account by address.
func (rs *rootResolver) Account(ctx context.Context, args struct{ Address common.Address }) (*Account, error) {
	// simply pull the block by hash
	acc, err := repository.R().Account(ctx, &args.Address)
	if err != nil {
		log.Errorf("could not get the specified account")
		return nil, err
	}
	return NewAccount(acc), nil
}

// AccountsActive resolves total number of active accounts on the blockchain.
func (rs *rootResolver) AccountsActive(ctx context.Context) (hexutil.Uint64, error) {
	return repository.R().AccountsActive(ctx)
}

// Balance resolves total balance of the account.
func (acc *Account) Balance() (hexutil.Big, error) {
	// get the balance
	val, err, _ := acc.cg.Do("balance", func() (interface{}, error) {
		return repository.R().AccountBalance(&acc.Address)
	})

	// can not get the balance?
	if err != nil {
		return hexutil.Big{}, err
	}
	return *val.(*hexutil.Big), nil
}

// TotalValue resolves the account total value.
//
// Stake delegation is not offered on this chain, so an account has no delegated stake, no
// delegation-bound pending rewards, and no pending un-delegations to add: the total value is simply
// the account balance. (This previously summed delegationsTotal over the delegation table, which is
// now uniformly zero and has been removed.)
func (acc *Account) TotalValue(_ context.Context) (hexutil.Big, error) {
	return acc.Balance()
}

// TxCount resolves the number of transaction sent by the account, also known as nonce.
func (acc *Account) TxCount() (hexutil.Uint64, error) {
	// get the sender by address
	bal, err := repository.R().AccountNonce(&acc.Address)
	if err != nil {
		return hexutil.Uint64(0), err
	}

	return *bal, nil
}

// TxList resolves list of transaction associated with the account.
func (acc *Account) TxList(ctx context.Context, args struct {
	Recipient *common.Address
	Cursor    *Cursor
	Count     int32
}) (*TransactionList, error) {
	// limit query size; the count can be either positive or negative
	// this controls the loading direction
	args.Count = listLimitCount(args.Count, accMaxTransactionsPerRequest)

	// get the transaction hash list from repository
	bl, err := repository.R().AccountTransactions(ctx, &acc.Address, args.Recipient, (*string)(args.Cursor), args.Count)
	if err != nil {
		return nil, err
	}

	return NewTransactionList(bl), nil
}

// Erc20TxList resolves list of ERC20 transactions associated with the account.
func (acc *Account) Erc20TxList(ctx context.Context, args struct {
	Cursor *Cursor
	Count  int32
	Token  *common.Address
	TxType *[]string
}) (*ERC20TransactionList, error) {
	// limit query size; the count can be either positive or negative
	// this controls the loading direction
	args.Count = listLimitCount(args.Count, accMaxTransactionsPerRequest)

	// get the transaction hash list from repository
	tl, err := repository.R().TokenTransactions(ctx,
		types.AccountTypeERC20Token,
		args.Token,
		nil,
		&acc.Address,
		ercTrxTypesFromNames(args.TxType),
		(*string)(args.Cursor),
		args.Count,
	)
	if err != nil {
		return nil, err
	}

	return NewERC20TransactionList(tl), nil
}

// Erc721TxList resolves list of ERC721 transactions associated with the account.
func (acc *Account) Erc721TxList(ctx context.Context, args struct {
	Cursor  *Cursor
	Count   int32
	Token   *common.Address
	TokenId *hexutil.Big
	TxType  *[]string
}) (*ERC721TransactionList, error) {
	// limit query size; the count can be either positive or negative
	// this controls the loading direction
	args.Count = listLimitCount(args.Count, accMaxTransactionsPerRequest)

	// get the transaction hash list from repository
	tl, err := repository.R().TokenTransactions(ctx,
		types.AccountTypeERC721Contract,
		args.Token,
		(*big.Int)(args.TokenId),
		&acc.Address,
		ercTrxTypesFromNames(args.TxType),
		(*string)(args.Cursor),
		args.Count,
	)
	if err != nil {
		return nil, err
	}

	return NewERC721TransactionList(tl), nil
}

// Erc1155TxList resolves list of ERC1155 transactions associated with the account.
func (acc *Account) Erc1155TxList(ctx context.Context, args struct {
	Cursor  *Cursor
	Count   int32
	Token   *common.Address
	TokenId *hexutil.Big
	TxType  *[]string
}) (*ERC1155TransactionList, error) {
	// limit query size; the count can be either positive or negative
	// this controls the loading direction
	args.Count = listLimitCount(args.Count, accMaxTransactionsPerRequest)

	// get the transaction hash list from repository
	tl, err := repository.R().TokenTransactions(ctx,
		types.AccountTypeERC1155Contract,
		args.Token,
		(*big.Int)(args.TokenId),
		&acc.Address,
		ercTrxTypesFromNames(args.TxType),
		(*string)(args.Cursor),
		args.Count,
	)
	if err != nil {
		return nil, err
	}

	return NewERC1155TransactionList(tl), nil
}

// Staker resolves the account staker detail, if the account is a staker.
func (acc *Account) Staker() (*Staker, error) {
	// get the staker
	st, err := repository.R().ValidatorByAddress(&acc.Address)
	if err != nil {
		return nil, err
	}

	// staker not found?
	if st == nil {
		return nil, nil
	}
	return NewStaker(st), nil
}

// Contract resolves the account smart contract detail,
// if the account is a smart contract address.
func (acc *Account) Contract(ctx context.Context) (*Contract, error) {
	// try to load contract details from repository first
	con, err := repository.R().Contract(ctx, &acc.Address)
	if err != nil {
		return nil, err
	}

	// contract not known in repository
	if con == nil {
		return nil, nil
	}

	return NewContract(con), nil
}

// TokenSummaries resolves all tokens (ERC20, fMint, ERC721, ERC1155, etc.) for the account.
func (acc *Account) TokenSummaries(ctx context.Context) ([]*repository.TokenSummary, error) {
	summaries, err := repository.R().TokenSummariesByAddress(ctx, acc.Address, 1000)
	if err != nil {
		return nil, err
	}
	out := make([]*repository.TokenSummary, len(summaries))
	for i := range summaries {
		out[i] = &summaries[i]
	}
	return out, nil
}
