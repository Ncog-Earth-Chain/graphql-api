// Package types implements different core types of the API.
package types

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"ncogearthchain-api-graphql/internal/repository/rpc/contracts"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Contract represents an Ncogearthchain smart contract at the blockchain.
type Contract struct {
	// Type represents a general type of the contract.
	Type string `json:"type"`

	// Address represents the address of the contract
	Address common.Address `json:"address"`

	// TransactionHash represents the hash of the contract deployment transaction.
	TransactionHash common.Hash `json:"tx"`

	// TimeStamp represents the unix timestamp of the contract deployment.
	TimeStamp hexutil.Uint64 `json:"timestamp"`

	// Name of the smart contract, if available.
	Name string `json:"name"`

	// Smart contract version identifier, if available.
	Version string `json:"ver,omitempty"`

	// SupportContact represents a contact to the smart contract support, if available.
	SupportContact string `json:"contact,omitempty"`

	// License represents an optional contact open source license
	// being used.
	License string `json:"license,omitempty"`

	// Smart contract compiler identifier, if available.
	Compiler string `json:"cv,omitempty"`

	// CompilerVersion represents the specific Solidity compiler version
	// used for validation, if available.
	CompilerVersion string `json:"cv_ver,omitempty"`

	// EvmVersion represents the target EVM version used for compilation, if any.
	EvmVersion string `json:"evm,omitempty"`

	// ViaIR specifies whether the compilation was performed via the Yul IR pipeline.
	ViaIR bool `json:"viaIR,omitempty"`

	// IsOptimized signals that the contract byte code was optimized
	// during compilation.
	IsOptimized bool `json:"optimized"`

	// OptimizeRuns represents number of optimization runs used
	// during the contract compilation.
	OptimizeRuns int32 `json:"optimizeRuns"`

	// SourceCode is the smart contract source code, if available.
	SourceCode string `json:"sol,omitempty"`

	// SourceCodeHash represents a hash code of the stored contract
	// source code. Is nil if the source code is not available.
	SourceCodeHash *common.Hash `json:"soh,omitempty"`

	// ABI definition of the smart contract, if available.
	Abi string `json:"abi,omitempty"`

	// Metadata is the Solidity compiler metadata JSON string produced during compilation, if available.
	Metadata string `json:"metadata,omitempty"`

	// Validated represents the unix timestamp
	//of the contract source validation against deployed byte code.
	Validated *hexutil.Uint64 `json:"ok,omitempty"`

	// CreationBytecode is the compiler-produced creation bytecode (hex), if available.
	CreationBytecode string `json:"creationBytecode,omitempty"`

	// RuntimeBytecode is the compiler-produced deployed/runtime bytecode (hex), if available.
	RuntimeBytecode string `json:"runtimeBytecode,omitempty"`

	// CreationLinkReferences holds link reference ranges in creation bytecode.
	CreationLinkReferences []LinkReferenceRange `json:"creationLinkReferences,omitempty"`

	// RuntimeLinkReferences holds link reference ranges in runtime bytecode.
	RuntimeLinkReferences []LinkReferenceRange `json:"runtimeLinkReferences,omitempty"`

	// RuntimeImmutableReferences holds immutable reference ranges in runtime bytecode.
	RuntimeImmutableReferences []LinkReferenceRange `json:"runtimeImmutableReferences,omitempty"`

	// IsProxy indicates whether this contract address is a proxy.
	IsProxy bool `json:"isProxy,omitempty"`

	// ProxyType describes the detected proxy mechanism (e.g., "EIP-1967", "Beacon", "EIP-1167").
	ProxyType string `json:"proxyType,omitempty"`

	// ImplementationAddress stores the resolved implementation address if this is a proxy.
	ImplementationAddress common.Address `json:"implementationAddress,omitempty"`

	// IsDDB indicates whether this contract was created via a DDB transaction.
	IsDDB bool `json:"isDDB,omitempty"`
}

// LinkReferenceRange represents a start/length pair within bytecode for linking/immutables.
type LinkReferenceRange struct {
	Start  int32 `json:"start"`
	Length int32 `json:"length"`
}

// UnmarshalContract parses the JSON-encoded smart contract data.
func UnmarshalContract(data []byte) (*Contract, error) {
	var sc Contract
	err := json.Unmarshal(data, &sc)
	return &sc, err
}

// Marshal returns the JSON encoding of Contract.
func (sc *Contract) Marshal() ([]byte, error) {
	return json.Marshal(sc)
}

// Uid generates unique identifier of the contract record.
func (sc *Contract) Uid() uint64 {
	return (uint64(sc.TimeStamp)&0xFFFFFFFFFF)<<24 | (binary.BigEndian.Uint64(sc.TransactionHash[:8]) & 0xFFFFFF)
}

// NewGenericContract creates new generic contract record
func NewGenericContract(addr *common.Address, block *Block, trx *Transaction) *Contract {
	// make the contract
	return &Contract{
		Type:             AccountTypeContract,
		Address:          *addr,
		TransactionHash:  trx.Hash,
		TimeStamp:        block.TimeStamp,
		Name:             "",
		Version:          "",
		SupportContact:   "",
		License:          "",
		Compiler:         "",
		IsOptimized:      false,
		OptimizeRuns:     0,
		SourceCode:       "",
		SourceCodeHash:   nil,
		Abi:              "",
		Metadata:         "",
		Validated:        nil,
		CreationBytecode: "",
		RuntimeBytecode:  "",
	}
}

// NewErcTokenContract creates new basic ERC20/ERC721/ERC1155 contract
func NewErcTokenContract(addr *common.Address, name string, block *Block, trx *Transaction, tType string, abi string) *Contract {
	// make the contract
	con := NewGenericContract(addr, block, trx)

	// set additional details
	con.Type = tType
	con.Abi = abi
	con.Name = name
	return con
}

// NewSfcContract creates new Special Purpose Contract reference
func NewSfcContract(addr *common.Address, ver uint64, block *Block, trx *Transaction) *Contract {
	// make the contract
	con := NewGenericContract(addr, block, trx)

	// set additional details
	con.Type = AccountTypeSFC
	con.Name = "SFC Contract"
	con.Version = fmt.Sprintf("%s.%s.%s",
		string([]byte{byte((ver >> 16) & 255)}),
		string([]byte{byte((ver >> 8) & 255)}),
		string([]byte{byte(ver & 255)}))
	con.SupportContact = "https://ncogchain.earth"
	con.License = "MIT"
	con.Compiler = "Solidity"
	con.SourceCode = "https://github.com/Ncog-Earth-Chain/ncogearthchain-sfc"
	con.Abi = contracts.SfcContractABI
	con.Validated = &block.TimeStamp
	return con
}

// NewStiContract creates new Staker Information Contract reference
func NewStiContract(addr *common.Address, block *Block, trx *Transaction) *Contract {
	// make the contract
	con := NewGenericContract(addr, block, trx)

	// set additional details
	con.Name = "Staker Info Contract"
	con.Version = "1.4.0"
	con.SupportContact = "https://github.com/Ncog-Earth-Chain/ncogearthchain-staker-info"
	con.License = "MIT"
	con.Compiler = "Solidity"
	con.SourceCode = "https://github.com/Ncog-Earth-Chain/ncogearthchain-staker-info"
	con.Abi = contracts.StakerInfoContractABI
	return con
}
