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
	"math/big"
	"ncogearthchain-api-graphql/internal/config"
	"ncogearthchain-api-graphql/internal/repository/db/pg"
	"ncogearthchain-api-graphql/internal/types"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	etc "github.com/ethereum/go-ethereum/core/types"
)

// Repository interface defines functions the underlying implementation provides to API resolvers.
type Repository interface {
	// Account returns account at Ncogearthchain blockchain for an address, nil if not found.
	Account(context.Context, *common.Address) (*types.Account, error)

	// AccountBalance returns the current balance of an account at Ncogearthchain blockchain.
	AccountBalance(*common.Address) (*hexutil.Big, error)

	// AccountNonce returns the current number of sent transactions of an account at Ncogearthchain blockchain.
	AccountNonce(*common.Address) (*hexutil.Uint64, error)

	// AccountTransactions returns list of transaction hashes for account at Ncogearthchain blockchain.
	//
	// String cursor represents cursor based on which the list is loaded. If null,
	// it loads either from top, or bottom of the list, based on the value
	// of the integer count. The integer represents the number of transaction loaded at most.
	//
	// For positive number, the list starts right after the cursor
	// (or on top without one) and loads at most defined number of transactions older than that.
	//
	// For negative number, the list starts right before the cursor
	// (or at the bottom without one) and loads at most defined number
	// of transactions newer than that.
	//
	// Transactions are always sorted from newer to older.
	AccountTransactions(context.Context, *common.Address, *common.Address, *string, int32) (*types.TransactionList, error)

	// AccountsActive total number of accounts known to repository.
	AccountsActive(ctx context.Context) (hexutil.Uint64, error)

	// AccountIsKnown checks if the account of the given address is known to the API server.
	AccountIsKnown(context.Context, *common.Address) bool

	// StoreAccount adds specified account detail into the repository.
	StoreAccount(context.Context, *types.Account) error

	// AccountMarkActivity marks the latest account activity in the repository.
	AccountMarkActivity(*common.Address, uint64) error

	// BlockHeight returns the current height of the Ncogearthchain blockchain in blocks.
	BlockHeight() (*hexutil.Big, error)

	// LastKnownBlock returns number of the last block known to the repository.
	LastKnownBlock(ctx context.Context) (uint64, error)

	// ObservedHeaders provides a channel fed with new headers observed
	// by the connected blockchain node.
	ObservedHeaders() chan *etc.Header

	// BlockByNumber returns a block at Ncogearthchain blockchain represented by a number.
	// Top block is returned if the number is not provided.
	// If the block is not found, ErrBlockNotFound error is returned.
	BlockByNumber(*hexutil.Uint64) (*types.Block, error)

	// BlockByHash returns a block at Ncogearthchain blockchain represented by a hash.
	// Top block is returned if the hash is not provided.
	// If the block is not found, ErrBlockNotFound error is returned.
	BlockByHash(*common.Hash) (*types.Block, error)

	// Blocks pulls list of blocks starting on the specified block number
	// and going up, or down based on count number.
	Blocks(*uint64, int32) (*types.BlockList, error)

	// CacheBlock puts a block to the internal block ring cache.
	CacheBlock(blk *types.Block)

	// Contract extract a smart contract information by address if available.
	Contract(context.Context, *common.Address) (*types.Contract, error)

	// Contracts returns list of smart contracts at Ncogearthchain blockchain.
	Contracts(context.Context, bool, *string, int32) (*types.ContractList, error)

	// ValidateContract tries to validate contract byte code using
	// provided source code. If successful, the contract information
	// is updated the the repository.
	ValidateContract(context.Context, *types.Contract) error

	// VerifyProxyContract verifies a proxy address in a BscScan-like flow.
	// If the address is a proxy and its implementation is not yet verified,
	// returns the parent (implementation) address and a message instructing to verify it first.
	// If the parent is already verified, links the proxy to the implementation, copies ABI/metadata,
	// marks the proxy as validated, and returns the updated proxy contract.
	VerifyProxyContract(context.Context, *common.Address) (*types.Contract, *common.Address, bool, string, error)

	// GetAvailableCompilerVersions returns a list of available Solidity compiler versions.
	GetAvailableCompilerVersions() []string

	// PreDownloadCompilerVersion downloads and installs a specific Solidity compiler version.
	PreDownloadCompilerVersion(version string) error

	// StoreContract updates the contract in repository.
	StoreContract(context.Context, *types.Contract) error

	// SfcVersion returns current version of the SFC contract.
	SfcVersion() (hexutil.Uint64, error)

	// SfcDecimalUnit returns the decimal unit adjustment used by the SFC contract.
	SfcDecimalUnit() *big.Int

	// CurrentEpoch returns the id of the current epoch.
	CurrentEpoch() (hexutil.Uint64, error)

	// LastKnownEpoch returns the id of the last known and scanned epoch.
	LastKnownEpoch(ctx context.Context) (uint64, error)

	// AddEpoch stores an epoch reference in connected persistent storage.
	AddEpoch(ctx context.Context, e *types.Epoch) error

	// Epoch returns the id of the current epoch.
	Epoch(*hexutil.Uint64) (*types.Epoch, error)

	// CurrentSealedEpoch returns the data of the latest sealed epoch.
	CurrentSealedEpoch() (*types.Epoch, error)

	// Epochs pulls list of epochs starting at the specified cursor.
	Epochs(ctx context.Context, cursor *string, count int32) (*types.EpochList, error)

	// TotalStaked calculates current total staked amount for all stakers.
	TotalStaked() (*hexutil.Big, error)

	// RewardsAllowed returns the reward lock status from SFC.
	RewardsAllowed() (bool, error)

	// LockingAllowed indicates if the stake locking has been enabled in SFC.
	LockingAllowed() (bool, error)

	// IsSfcContract returns true if the given address points to the SFC contract.
	IsSfcContract(*common.Address) bool

	// IsStiContract returns true if the given address points to the STI contract.
	IsStiContract(*common.Address) bool

	// StoreBlockAtomic stores a block and all of its transactions in one database
	// transaction, advancing the ingest watermark inside it.
	StoreBlockAtomic(context.Context, *types.Block, []*types.Transaction) error

	// LoadTransaction returns a transaction at Ncogearthchain blockchain
	// by a hash loaded directly from the node.
	LoadTransaction(hash *common.Hash) (*types.Transaction, error)

	// Transaction returns a transaction at Ncogearthchain blockchain by a hash, nil if not found.
	Transaction(*common.Hash) (*types.Transaction, error)

	// Transactions returns list of transaction hashes at Ncogearthchain blockchain.
	Transactions(context.Context, *string, int32) (*types.TransactionList, error)

	// TransactionsCount returns total number of transactions in the block chain.
	TransactionsCount(ctx context.Context) (uint64, error)

	// EstimateTransactionsCount returns an approximate amount of transactions on the network.
	EstimateTransactionsCount() (hexutil.Uint64, error)

	// IncTrxCountEstimate bumps the value of transaction counter estimator.
	IncTrxCountEstimate(diff uint64)

	// UpdateTrxCountEstimate updates the value of transaction counter estimator.
	UpdateTrxCountEstimate(val uint64)

	// CacheTransaction puts a transaction to the internal ring cache.
	CacheTransaction(trx *types.Transaction)

	// SendTransaction sends raw signed and RLP encoded transaction to the block chain.
	SendTransaction(hexutil.Bytes) (*types.Transaction, error)

	// LastValidatorId returns the last validator id in Ncogearthchain blockchain.
	LastValidatorId() (uint64, error)

	// ValidatorsCount returns the number of stakers in Ncogearthchain blockchain.
	ValidatorsCount() (uint64, error)

	// IsValidator returns TRUE if the given address is an SFC staker.
	IsValidator(*common.Address) (bool, error)

	// ValidatorAddress extract a staker address for the given staker ID.
	ValidatorAddress(*hexutil.Big) (*common.Address, error)

	// Validator extract a staker information from SFC smart contract.
	Validator(*hexutil.Big) (*types.Validator, error)

	// ValidatorByAddress extract a staker information by address.
	ValidatorByAddress(*common.Address) (*types.Validator, error)

	// ValidatorDowntime pulls information about validator downtime from the RPC interface.
	ValidatorDowntime(*hexutil.Big) (uint64, uint64, error)

	// SfcConfiguration provides SFC contract configuration.
	SfcConfiguration() (*types.SfcConfig, error)

	// SfcMaxDelegatedRatio extracts a ratio between self delegation and received stake.
	SfcMaxDelegatedRatio() (*big.Int, error)

	// PullStakerInfo extracts an extended staker information from smart contact.
	PullStakerInfo(*hexutil.Big) (*types.StakerInfo, error)

	// StoreStakerInfo stores staker information to in-memory cache for future use.
	StoreStakerInfo(*hexutil.Big, *types.StakerInfo) error

	// RetrieveStakerInfo gets staker information from in-memory if available.
	RetrieveStakerInfo(*hexutil.Big) *types.StakerInfo

	// IsDelegating returns if the given address is an SFC delegator.
	IsDelegating(context.Context, *common.Address) (bool, error)

	// StoreDelegation stores a delegation in the persistent repository.
	StoreDelegation(context.Context, *types.Delegation) error

	// UpdateDelegationBalance updates active balance of the given delegation.
	UpdateDelegationBalance(context.Context, *common.Address, *hexutil.Big, func(*big.Int) error) error

	// Delegation returns a detail of delegation for the given address and validator ID.
	Delegation(context.Context, *common.Address, *hexutil.Big) (*types.Delegation, error)

	// DelegationAmountStaked returns the current amount of staked tokens
	// for the given delegation.
	DelegationAmountStaked(*common.Address, *hexutil.Big) (*big.Int, error)

	// DelegationsByAddress returns a list of all delegations of a given delegator address.
	DelegationsByAddress(context.Context, *common.Address, *string, int32) (*types.DelegationList, error)

	// DelegationsByAddressAll returns a list of all delegations of the given address un-paged.
	DelegationsByAddressAll(ctx context.Context, addr *common.Address) ([]*types.Delegation, error)

	// DelegationsOfValidator extracts a list of delegations for a validator by its ID.
	DelegationsOfValidator(context.Context, *hexutil.Big, *string, int32) (*types.DelegationList, error)

	// DelegationLock returns delegation lock information using SFC contract binding.
	DelegationLock(*common.Address, *hexutil.Big) (*types.DelegationLock, error)

	// DelegationUnlockPenalty returns the amount of penalty applied on given stake unlock.
	DelegationUnlockPenalty(addr *common.Address, valID *big.Int, amount *big.Int) (hexutil.Big, error)

	// DelegationAmountUnlocked returns delegation lock information using SFC contract binding.
	DelegationAmountUnlocked(addr *common.Address, valID *big.Int) (hexutil.Big, error)

	// PendingRewards returns a detail of pending rewards for the given delegation.
	PendingRewards(*common.Address, *hexutil.Big) (*types.PendingRewards, error)

	// DelegationOutstandingSNEC returns the amount of sNEC tokens for the delegation
	// identified by the delegator address and the staker id.
	DelegationOutstandingSNEC(*common.Address, *hexutil.Big) (*hexutil.Big, error)

	// DelegationTokenizerUnlocked returns the status of SFC Tokenizer lock
	// for a delegation identified by the address and staker id.
	DelegationTokenizerUnlocked(*common.Address, *hexutil.Big) (bool, error)

	// DelegationFluidStakingActive signals if the delegation is upgraded to Fluid Staking model.
	DelegationFluidStakingActive(*common.Address, *hexutil.Big) (bool, error)

	// StoreWithdrawRequest stores the given withdraw request in persistent storage.
	StoreWithdrawRequest(context.Context, *types.WithdrawRequest) error

	// UpdateWithdrawRequest stores the updated withdraw request in persistent storage.
	UpdateWithdrawRequest(context.Context, *types.WithdrawRequest) error

	// WithdrawRequest extracts details of a withdraw request specified by the delegator, validator and request ID.
	WithdrawRequest(context.Context, *common.Address, *hexutil.Big, *hexutil.Big) (*types.WithdrawRequest, error)

	// WithdrawRequests extracts a list of withdraw requests for the given address and validator.
	WithdrawRequests(context.Context, *common.Address, *hexutil.Big, *string, int32) (*types.WithdrawRequestList, error)

	// WithdrawRequestsPendingTotal is the total value of all pending withdrawal requests
	// for the given delegator and target staker ID.
	WithdrawRequestsPendingTotal(context.Context, *common.Address, *hexutil.Big) (*big.Int, error)

	// StoreRewardClaim stores reward claim record in the persistent repository.
	StoreRewardClaim(context.Context, *types.RewardClaim) error

	// RewardsClaimed returns the sum of all the claimed rewards
	// for the given delegator address and validator ID.
	RewardsClaimed(ctx context.Context, adr *common.Address, valId *big.Int, since *int64, until *int64) (*big.Int, error)

	// RewardClaims provides list of reward claims for the given criteria.
	RewardClaims(context.Context, *common.Address, *big.Int, *string, int32) (*types.RewardClaimsList, error)

	// Price returns a price information for the given target symbol.
	Price(sym string) (types.Price, error)

	// GasPrice provides the raw suggested value for the gas price.
	GasPrice() (hexutil.Big, error)

	// GasPriceExtended provides extended gas price information.
	GasPriceExtended() (*types.GasPrice, error)

	// StoreGasPricePeriod stores gas price period data into the persistent storage.
	StoreGasPricePeriod(context.Context, *types.GasPricePeriod) error

	// GasEstimate calculates the estimated amount of Gas required to perform
	// transaction described by the input params.
	GasEstimate(*struct {
		From  *common.Address
		To    *common.Address
		Value *hexutil.Big
		Data  *string
	}) (*hexutil.Uint64, error)

	// TokenTransactions provides list of ERC20/ERC721/ERC1155 transactions based on given filters.
	TokenTransactions(ctx context.Context, tokenType string, token *common.Address, tokenId *big.Int, acc *common.Address, txType []int32, cursor *string, count int32) (*types.TokenTransactionList, error)

	// TokenTransactionsByCall provides a list of token transaction made inside a specific
	// transaction call (blockchain transaction).
	TokenTransactionsByCall(context.Context, *common.Hash) ([]*types.TokenTransaction, error)

	// Erc20Token returns an ERC20 token for the given address, if available.
	Erc20Token(*common.Address) (*types.Erc20Token, error)

	// Erc20TokensList returns a list of known ERC20 tokens ordered by their activity.
	Erc20TokensList(context.Context, int32) ([]common.Address, error)

	// Erc20Assets provides list of ERC20 tokens involved with the given owner.
	Erc20Assets(context.Context, common.Address, int32) ([]common.Address, error)

	// Erc20BalanceOf load the current available balance of and ERC20 token identified by the token
	// contract address for an identified owner address.
	Erc20BalanceOf(*common.Address, *common.Address) (hexutil.Big, error)

	// Erc20Allowance loads the current amount of ERC20 tokens the owner has
	// unlocked for the given spender.
	Erc20Allowance(*common.Address, *common.Address, *common.Address) (hexutil.Big, error)

	// Erc20TotalSupply provides information about all available tokens
	Erc20TotalSupply(*common.Address) (hexutil.Big, error)

	// Erc20Name provides information about the name of the ERC20 token.
	Erc20Name(*common.Address) (string, error)

	// Erc20Symbol provides information about the symbol of the ERC20 token.
	Erc20Symbol(*common.Address) (string, error)

	// Erc20Decimals provides information about the decimals of the ERC20 token.
	Erc20Decimals(*common.Address) (int32, error)

	// Erc20LogoURL provides URL address of a logo of the ERC20 token.
	Erc20LogoURL(*common.Address) string

	// StoreTokenTransaction stores ERC20/ERC721/ERC1155 transaction into the repository.
	StoreTokenTransaction(context.Context, *types.TokenTransaction) error

	// Erc165SupportsInterface provides information about support of the interface by the contract.
	Erc165SupportsInterface(contract *common.Address, interfaceID [4]byte) (bool, error)

	// Erc721Contract returns an ERC721 token for the given address, if available.
	Erc721Contract(*common.Address) (*types.Erc721Contract, error)

	// Erc721ContractsList returns a list of known ERC721 tokens ordered by their activity.
	Erc721ContractsList(context.Context, int32) ([]common.Address, error)

	// Erc721Name provides information about the name of the ERC721 token.
	Erc721Name(*common.Address) (string, error)

	// Erc721Symbol provides information about the symbol of the ERC721 token.
	Erc721Symbol(*common.Address) (string, error)

	// Erc721TotalSupply provides information about all available tokens.
	Erc721TotalSupply(token *common.Address) (hexutil.Big, error)

	// Erc721BalanceOf provides amount of NFT tokens owned by given owner in given ERC721 contract.
	Erc721BalanceOf(token *common.Address, owner *common.Address) (hexutil.Big, error)

	// Erc721TokenURI provides URI of Metadata JSON Schema of the ERC721 token.
	Erc721TokenURI(token *common.Address, tokenId *big.Int) (string, error)

	// Erc721OwnerOf provides information about NFT token ownership.
	Erc721OwnerOf(token *common.Address, tokenId *big.Int) (common.Address, error)

	// Erc721GetApproved provides information about operator approved to manipulate with the NFT token.
	Erc721GetApproved(token *common.Address, tokenId *big.Int) (common.Address, error)

	// Erc721IsApprovedForAll provides information about operator approved to manipulate with NFT tokens of given owner.
	Erc721IsApprovedForAll(token *common.Address, owner *common.Address, operator *common.Address) (bool, error)

	// Erc1155ContractsList returns a list of known ERC1155 contracts ordered by their activity.
	Erc1155ContractsList(context.Context, int32) ([]common.Address, error)

	// Erc1155Uri provides URI of Metadata JSON Schema of the token.
	Erc1155Uri(token *common.Address, tokenId *big.Int) (string, error)

	// Erc1155BalanceOf provides amount of NFT tokens owned by given owner.
	Erc1155BalanceOf(token *common.Address, owner *common.Address, tokenId *big.Int) (*big.Int, error)

	// Erc1155BalanceOfBatch provides amount of NFT tokens owned by given owner.
	Erc1155BalanceOfBatch(token *common.Address, owners *[]common.Address, tokenIds []*big.Int) ([]*big.Int, error)

	// Erc1155IsApprovedForAll provides information about operator approved to manipulate with NFT tokens of given owner.
	Erc1155IsApprovedForAll(token *common.Address, owner *common.Address, operator *common.Address) (bool, error)

	// GovernanceContractBy provides governance contract details by its address.
	GovernanceContractBy(*common.Address) (*config.GovernanceContract, error)

	// GovernanceProposalsCount provides the total number of proposals
	// in a given Governance contract.
	GovernanceProposalsCount(*common.Address) (hexutil.Big, error)

	// GovernanceProposal provides a detail of Proposal of a governance contract
	// specified by its id.
	GovernanceProposal(*common.Address, *hexutil.Big) (*types.GovernanceProposal, error)

	// GovernanceProposalState provides a state of Proposal of a governance contract
	// specified by its id.
	GovernanceProposalState(*common.Address, *hexutil.Big) (*types.GovernanceProposalState, error)

	// GovernanceOptionState returns a state of the given option of a proposal.
	GovernanceOptionState(*common.Address, *hexutil.Big, *hexutil.Big) (*types.GovernanceOptionState, error)

	// GovernanceOptionStates returns a list of states of options of a proposal.
	GovernanceOptionStates(*common.Address, *hexutil.Big, int) ([]*types.GovernanceOptionState, error)

	// GovernanceVote provides a single vote in the Governance Proposal context.
	GovernanceVote(*common.Address, *hexutil.Big, *common.Address, *common.Address) (*types.GovernanceVote, error)

	// GovernanceProposals loads list of proposals from given set of Governance contracts.
	GovernanceProposals([]*common.Address, *string, int32, bool) (*types.GovernanceProposalList, error)

	// GovernanceProposalFee returns the fee payable for a new proposal
	// in given Governance contract context.
	GovernanceProposalFee(*common.Address) (hexutil.Big, error)

	// GovernanceTotalWeight provides the total weight of all available votes
	// in the governance contract identified by the address.
	GovernanceTotalWeight(*common.Address) (hexutil.Big, error)

	// DdbOperations returns the on-chain DDB operation history.
	//
	// The node serves NO DDB history RPC, so this is the only place it exists.
	DdbOperations(ctx context.Context, c pg.DdbOpCriteria, cursor *string, count int32) ([]*types.DdbOperation, error)

	// DdbOperationAt returns the DDB operation committed at a block position.
	DdbOperationAt(ctx context.Context, blockNumber, txIndex uint64) (*types.DdbOperation, error)

	// DdbContracts returns the known data contracts, most recently active first,
	// cursor-paginated.
	DdbContracts(ctx context.Context, cursor *string, count int32) ([]*types.DdbContract, error)

	// DdbContract returns a single data contract by address, or nil if unknown.
	DdbContract(ctx context.Context, addr common.Address) (*types.DdbContract, error)

	// Logs returns event logs matching the criteria, newest first.
	//
	// MongoDB stored logs inside the transaction document and indexed nothing about them,
	// so no log query was possible at all. Every filter combination the criteria can
	// express is served by an index.
	Logs(ctx context.Context, c pg.LogCriteria, cursor *string, count int32) ([]*types.Log, error)

	// LogsByTransaction returns every log a transaction emitted, in emission order.
	LogsByTransaction(ctx context.Context, txHash *common.Hash) ([]*types.Log, error)

	// RawTransaction fetches a transaction's canonical RLP encoding from the node.
	//
	// This is what makes storing ML-DSA public keys unnecessary: the encoding carries the
	// signature and public key, and keccak256 of it equals the transaction hash, so the
	// credentials are verifiable against a value this explorer already holds.
	RawTransaction(ctx context.Context, hash *common.Hash) ([]byte, error)

	// DecodeRawTransaction extracts the signature, public key and claimed sender from a
	// raw transaction encoding.
	DecodeRawTransaction(ctx context.Context, raw []byte) ([]byte, []byte, common.Address, error)

	// TraceBlockByNumber traces a block by its number.
	TraceBlockByNumber(ctx context.Context, number hexutil.Uint64, params map[string]interface{}) (interface{}, error)

	// TraceBlockByHash traces a block by its hash.
	TraceBlockByHash(ctx context.Context, hash common.Hash, params map[string]interface{}) (interface{}, error)

	// TraceTransaction traces a transaction.
	TraceTransaction(ctx context.Context, hash common.Hash, params map[string]interface{}) (interface{}, error)

	// --- DDB (Decentralized DataBase) introspection ---

	// DdbValidators returns the current DDB validator committee + threshold.
	DdbValidators() (interface{}, error)

	// DdbSchema returns a contract schema's definition (tables, procedures, roles).
	DdbSchema(schemaName string) (interface{}, error)

	// DdbSelect returns rows from a table with optional filters / ordering / pagination.
	DdbSelect(schemaName, tableName string, options interface{}) (interface{}, error)

	// DdbQuery returns up to limit rows from a table.
	DdbQuery(schemaName, tableName string, limit int) (interface{}, error)

	// DdbStats returns DDB storage stats.
	DdbStats() (interface{}, error)

	// DdbConsensusStats returns dual-consensus stats.
	DdbConsensusStats() (interface{}, error)

	// DdbEndorsementStatus returns the endorsement status for a request id.
	DdbEndorsementStatus(requestID common.Hash) (interface{}, error)

	// TrxFlowVolume resolves the list of daily trx flow aggregations.
	TrxFlowVolume(ctx context.Context, from *time.Time, to *time.Time) ([]*types.DailyTrxVolume, error)

	// TrxGasSpeed provides speed of gas consumption per second by transactions.
	TrxGasSpeed(ctx context.Context, from *time.Time, to *time.Time) (float64, error)

	// GasPriceTicks provides a list of gas price ticks for the given time period.
	GasPriceTicks(ctx context.Context, from *time.Time, to *time.Time) ([]types.GasPricePeriod, error)

	// TrxFlowUpdate executes the trx flow update in the database.
	TrxFlowUpdate(ctx context.Context)

	// TrxFlowSpeed provides speed of transaction per second for the last <sec> seconds.
	TrxFlowSpeed(ctx context.Context, sec int32) (float64, error)

	// StoreNecBurn stores the given native NEC burn per block record into the persistent storage.
	StoreNecBurn(ctx context.Context, burn *types.NecBurn) error

	// NecBurnTotal provides the total amount of burned native NEC.
	NecBurnTotal(ctx context.Context) (int64, error)

	// NecBurnList provides list of per-block burned native NEC tokens.
	NecBurnList(ctx context.Context, count int64) ([]types.NecBurn, error)

	// RefreshAccountStats rebuilds the account_stat materialized view that backs account
	// transaction counts and the "most active" token lists. It must be run on a schedule;
	// nothing else keeps it current, so without it those figures stay frozen at migration.
	RefreshAccountStats(ctx context.Context) error

	// MaintainPartitions extends the time- and block-range partitions ahead of the given
	// head and prunes those past retention. It is idempotent and must be run on a schedule:
	// the migrations seed a fixed runway once and nothing else advances it, so gas-price
	// inserts eventually fail and tx_log rows fall into an unprunable default partition.
	MaintainPartitions(ctx context.Context, head uint64) error

	// Close and cleanup the repository.
	Close()

	// TokenSummariesByAddress aggregates all token types for a wallet address.
	TokenSummariesByAddress(ctx context.Context, addr common.Address, count int32) ([]TokenSummary, error)

	// Erc721Assets returns all ERC721 contracts where the owner has a balance > 0.
	Erc721Assets(ctx context.Context, owner common.Address, count int32) ([]common.Address, error)
}
