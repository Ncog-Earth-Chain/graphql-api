/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.

Internally it utilizes RPC to access Ncogearthchain/Forest full node for blockchain interaction. Mongo database
for fast, robust and scalable off-chain data storage, especially for aggregated and pre-calculated data mining
results. BigCache for in-memory object storage to speed up loading of frequently accessed entities.
*/
package repository

import (
	"context"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Account returns account at Ncogearthchain blockchain for an address, nil if not found.
func (p *proxy) Account(addr *common.Address) (acc *types.Account, err error) {
	// try to get the account from cache
	acc = p.cache.PullAccount(addr)

	// we still don't know the account? try to manually construct it if possible
	if acc == nil {
		acc, err = p.getAccount(addr)
		if err != nil {
			return nil, err
		}
	}

	// return the account
	return acc, nil
}

// Account returns account at Ncogearthchain blockchain for an address, nil if not found.
// func (p *proxy) AccountPost(addr *common.Address) (*types.Account, error) {
// 	// Try to get the account from cache
// 	acc := p.cache.PullAccount(addr)

// 	// If the account is not in the cache, try to fetch it from the database
// 	if acc == nil {
// 		var err error
// 		acc, err = p.getAccountPost(addr)
// 		if err != nil {
// 			return nil, err
// 		}
// 	}

// 	// Return the account
// 	return acc, nil
// }

// getAccount builds the account representation after validating it against Forest node.
func (p *proxy) getAccount(addr *common.Address) (*types.Account, error) {
	// any address given?
	if addr == nil {
		p.log.Error("no address given")
		return nil, fmt.Errorf("no address given")
	}

	// try to get the account from database first
	acc, err := p.db.Account(addr)
	if err != nil {
		p.log.Errorf("can not get the account %s; %s", addr.String(), err.Error())
		return nil, err
	}

	// found the account in database?
	if acc == nil {
		// log an unknown address
		p.log.Debugf("unknown address %s detected", addr.String())

		// at least we know the account existed
		acc = &types.Account{Address: *addr, Type: types.AccountTypeWallet}

		// check if this is a smart contract account; we log the error on the call
		acc.ContractTx, _ = p.db.ContractTransaction(addr)
	}

	// also keep a copy at the in-memory cache
	if err = p.cache.PushAccount(acc); err != nil {
		p.log.Warningf("can not keep account [%s] information in memory; %s", addr.Hex(), err.Error())
	}
	return acc, nil
}

func (p *proxy) getAccountsFromBlockchain() ([]common.Address, error) {
	p.log.Infof("Fetching accounts from blockchain via RPC")

	var accounts []string
	err := p.rpc.Rpc.CallContext(context.Background(), &accounts, "nec_accounts")
	if err != nil {
		p.log.Errorf("Failed to fetch accounts from blockchain: %v", err)
		return nil, err
	}

	p.log.Infof("Fetched accounts from blockchain: %v", accounts)

	var result []common.Address
	for _, acc := range accounts {
		address := common.HexToAddress(acc)
		result = append(result, address)
	}
	return result, nil
}

// AccountBalance returns the current balance of an account at Ncogearthchain blockchain.
func (p *proxy) AccountBalance(addr *common.Address) (*hexutil.Big, error) {
	return p.rpc.AccountBalance(addr)
}

// AccountNonce returns the current number of sent transactions of an account at Ncogearthchain blockchain.
func (p *proxy) AccountNonce(addr *common.Address) (*hexutil.Uint64, error) {
	return p.rpc.AccountNonce(addr)
}

// AccountTransactions returns slice of AccountTransaction structure for a given account at Ncogearthchain blockchain.
// func (p *proxy) AccountTransactions(addr *common.Address, rec *common.Address, cursor *string, count int32) (*types.TransactionList, error) {
// 	// do we have an account?
// 	if addr == nil {
// 		return nil, fmt.Errorf("can not get transaction list for empty account")
// 	}

// 	// go to the database for the list of hashes of transaction searched
// 	return p.db.AccountTransactions(addr, rec, cursor, count)
// }

// AccountTransactions returns a slice of AccountTransaction structures for a given account at Ncogearthchain blockchain.
func (p *proxy) AccountTransactions(addr *common.Address, rec *common.Address, cursor *string, count int32) (*types.TransactionList, error) {
	// Validate the input address
	if addr == nil {
		return nil, fmt.Errorf("cannot get transaction list for an empty account")
	}

	// Convert rec (if it's not nil) to a string, otherwise pass nil
	var recStr *string
	if rec != nil {
		recStr = new(string)
		*recStr = rec.Hex() // Convert the *common.Address to string and assign to recStr
	}

	// Fetch the transaction list from PostgreSQL
	postTxnList, err := p.pdDB.AccountTransactions(addr.Hex(), recStr, cursor, count)
	if err != nil {
		return nil, err
	}

	// Convert PostTransactionList to TransactionList
	txnList := &types.TransactionList{
		Collection: make([]*types.Transaction, len(postTxnList.Collection)),
	}

	// Loop over the transactions in the PostTransactionList and map to AccountTransaction
	for i, postTxn := range postTxnList.Collection {
		txnList.Collection[i] = &types.Transaction{
			Hash:      postTxn.Hash,
			From:      postTxn.From,
			To:        postTxn.To,
			Value:     postTxn.Value,
			TimeStamp: postTxn.TimeStamp,
		}
	}

	// Return the populated TransactionList
	return txnList, nil
}

// // AccountTransactionsPost returns a slice of AccountTransaction structures for a given account at Ncogearthchain blockchain.
// func (p *proxy) AccountTransactionsPost(addr string, rec *string, cursor *string, count int32) (*types.PostTransactionList, error) {
// 	// Validate the input address
// 	if addr == "" {
// 		return nil, fmt.Errorf("cannot get transaction list for an empty account")
// 	}

// 	// Fetch the transaction list from PostgreSQL
// 	postTxnList, err := p.pdDB.AccountTransactions(addr, rec, cursor, count)
// 	if err != nil {
// 		return nil, err
// 	}

// 	// Convert PostTransactionList to TransactionList
// 	txnList := &types.PostTransactionList{
// 		Collection: make([]*types.Transaction, len(postTxnList.Collection)),
// 	}

// 	// Iterate over fetched transactions and map them to the Transaction struct
// 	for i, postTxn := range postTxnList.Collection {
// 		txnList.Collection[i] = &types.Transaction{
// 			Hash:              postTxn.Hash,
// 			From:              postTxn.From,
// 			To:                postTxn.To,
// 			Value:             postTxn.Value,
// 			Gas:               postTxn.Gas,
// 			GasPrice:          postTxn.GasPrice,
// 			Nonce:             postTxn.Nonce,
// 			InputData:         postTxn.InputData,
// 			BlockHash:         postTxn.BlockHash,
// 			BlockNumber:       postTxn.BlockNumber,
// 			TimeStamp:         postTxn.TimeStamp,
// 			ContractAddress:   postTxn.ContractAddress,
// 			TrxIndex:          postTxn.TrxIndex,
// 			CumulativeGasUsed: postTxn.CumulativeGasUsed,
// 			GasUsed:           postTxn.GasUsed,
// 			Status:            postTxn.Status,
// 		}
// 	}

// 	return txnList, nil
// }

// AccountsActive returns total number of accounts known to repository.
func (p *proxy) AccountsActive() (hexutil.Uint64, error) {
	val, err := p.pdDB.AccountCount()
	return hexutil.Uint64(val), err
}

// // AccountsActive returns total number of accounts known to repository.
// func (p *proxy) AccountsActivePost() (hexutil.Uint64, error) {
// 	// Query the PostgreSQL database to count the active accounts
// 	count, err := p.pdDB.AccountCount()
// 	if err != nil {
// 		return 0, err
// 	}

// 	return hexutil.Uint64(count), nil
// }

// AccountIsKnown checks if the account of the given address is known to the API server.
func (p *proxy) AccountIsKnown(addr *common.Address) bool {
	// try cache first
	stat := p.cache.CheckAccountKnown(addr)
	if nil != stat {
		return *stat
	}

	// check if the database knows the address
	known, err := p.pdDB.IsAccountKnown(addr)
	if err != nil {
		p.log.Errorf("can not check account %s existence; %s", addr.String(), err.Error())
		return false
	}

	// if the account is known already, mark it in cache for faster resolving
	if known {
		p.cache.PushAccountKnown(addr)
	}
	return known
}

// AccountIsKnown checks if the account of the given address is known to the API server.
// func (p *proxy) AccountIsKnownPost(addr *common.Address) bool {
// 	// try cache first
// 	stat := p.cache.CheckAccountKnown(addr)
// 	if stat != nil {
// 		return *stat
// 	}

// 	// check if the database knows the address
// 	known, err := p.pdDB.IsAccountKnown(addr)
// 	if err != nil {
// 		p.log.Errorf("can not check account %s existence; %s", addr.String(), err.Error())
// 		return false
// 	}

// 	// if the account is known already, mark it in cache for faster resolving
// 	if known {
// 		p.cache.PushAccountKnown(addr)
// 	}
// 	return known
// }

func (p *proxy) StoreAccount(acc *types.Account) error {
	p.log.Infof("Fetching accounts from blockchain to store in DB")

	// Fetch accounts from blockchain
	blockchainAccounts, err := p.getAccountsFromBlockchain()
	if err != nil {
		p.log.Errorf("Error fetching accounts from blockchain: %v", err)
		return err
	}

	// Loop through fetched accounts and store them
	for _, acc := range blockchainAccounts {
		p.log.Infof("Processing blockchain account: %s", acc.Hex())

		newAccount := &types.Account{
			Address: acc,
			Type:    "wallet", // Adjust if needed
		}

		// Store in PostgreSQL
		err := p.pdDB.AddAccount(newAccount)
		if err != nil {
			p.log.Errorf("Error storing account %s in DB: %v", acc.Hex(), err)
		} else {
			p.log.Infof("Account %s successfully stored in PostgreSQL!", acc.Hex())
		}
	}

	return nil
}

// StoreAccount adds specified account detail into the repository.
// func (p *proxy) StoreAccountPost(acc *types.Account) error {
// 	// Add this account to the PostgreSQL database
// 	err := p.pdDB.AddAccount(acc)
// 	if err == nil {
// 		// If successful, remember that it's been added in the cache
// 		p.cache.PushAccountKnown(&acc.Address)
// 	}
// 	return err
// }

// AccountMarkActivity marks the latest account activity in the repository.
func (p *proxy) AccountMarkActivity(addr *common.Address, timestamp uint64) error {
	return p.pdDB.AccountMarkActivity(addr, timestamp)
}

// // AccountMarkActivity marks the latest account activity in the repository.
// func (p *proxy) AccountMarkActivityPost(addr *common.Address, ts uint64) error {
// 	// Call the PostgreSQL bridge to mark the account activity
// 	return p.pdDB.AccountMarkActivity(addr, ts)
// }
