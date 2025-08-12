/*
Package repository implements repository for handling fast and efficient access to data required
by the resolvers of the API server.

Internally it utilizes RPC to access Ncogearthchain/Forest full node for blockchain interaction. Mongo database
for fast, robust and scalable off-chain data storage, especially for aggregated and pre-calculated data mining
results. BigCache for in-memory object storage to speed up loading of frequently accessed entities.
*/
package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"ncogearthchain-api-graphql/internal/types"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/compiler"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Contract extract a smart contract information by account address, if available.
func (p *proxy) Contract(addr *common.Address) (*types.Contract, error) {
	// try cache first
	sc := p.cache.PullContract(addr)

	// we still don't know the contract? call the db for that
	if sc == nil {
		var err error
		sc, err = p.db.Contract(addr)
		if err != nil {
			return nil, err
		}

		// found the contract? push to cache for future use
		if sc != nil {
			if err = p.cache.PushContract(sc); err != nil {
				p.log.Criticalf("can not cache contract %s; %s", addr.String(), err.Error())
			}
		}
	}

	return sc, nil
}

// Contracts returns list of smart contracts at Ncogearthchain blockchain.
func (p *proxy) Contracts(validatedOnly bool, cursor *string, count int32) (*types.ContractList, error) {
	// go to the database for the list of contracts searched
	return p.db.Contracts(validatedOnly, cursor, count)
}

// cutCodeMetadata removes the IPFS/Swarm metadata information from the code
// for partial comparison. The current version of the Solidity compiler usually
// adds metadata to the end of the deployed byte code.
// @see https://solidity.readthedocs.io/en/latest/metadata.html
func cutCodeMetadata(bc []byte) []byte {
	// last 2 bytes are expected to contain metadata length
	bcLen := uint64(len(bc))
	cut := uint64(bc[bcLen-2])<<8 | uint64(bc[bcLen-1])

	// are we safely within the byte code size?
	if cut == 0 || cut >= bcLen-2 {
		return bc
	}

	return bc[:bcLen-cut-2]
}

// compareContractCode compares provided compiled code with the transaction input.
func compareContractCode(tx *types.Transaction, code string) (bool, error) {
	// decode the detail into byte array
	bc, err := hexutil.Decode(code)
	if err != nil {
		return false, err
	}

	// remove meta data hash from the byte code so we can compare raw
	// contract byte content. Such comparison is not perfect since
	// there could be changes in the source code not reflected
	// in the byte code. (variables renamed, unused code introduced, etc.)
	// Safer would be to use full CBOR parser here.
	bc = cutCodeMetadata(bc)

	// Is the transaction input shorter than the compiled contract?
	// If so there is no chance for pass.
	if len(tx.InputData) < len(bc) {
		return false, nil
	}

	// compare only up to <bc> length, the rest is metadata
	// and constructor parameters
	res := bytes.Compare(bc, tx.InputData[:len(bc)])

	// return the comparison result
	return res == 0, nil
}

// updateContractDetails updates local contract details from the provided compiler
// output.
func updateContractDetails(sc *types.Contract, detail *compiler.Contract) {
	// copy compiler information
	var str strings.Builder
	str.WriteString(detail.Info.Language)
	str.WriteString(" ")
	str.WriteString(detail.Info.LanguageVersion)
	sc.Compiler = str.String()

	// copy ABI
	abi, err := json.Marshal(detail.Info.AbiDefinition)
	if err == nil {
		sc.Abi = string(abi)
	}

	// copy the source code
	sc.SourceCode = detail.Info.Source
}

// ValidateContract tries to validate contract byte code using
// provided source code. If successful, the contract information
// is updated the the repository.
func (p *proxy) ValidateContract(sc *types.Contract) error {
	// get the byte code of the actual contract
	tx, err := p.Transaction(&sc.TransactionHash)
	if err != nil {
		p.log.Errorf("can not get contract deployment transaction; %s", err.Error())
		return err
	}

	// determine which compiler to use
	compilerPath := p.solCompiler
	if sc.CompilerVersion != "" {
		// try to get the specific compiler version
		p.log.Infof("requesting Solidity compiler version %s for contract validation", sc.CompilerVersion)
		specificPath, err := p.compilerMgr.GetCompilerPath(sc.CompilerVersion)
		if err != nil {
			p.log.Errorf("solidity compiler version %s not available, using default: %s", sc.CompilerVersion, err.Error())
		} else {
			compilerPath = specificPath
			p.log.Infof("using solidity compiler version %s at %s", sc.CompilerVersion, compilerPath)
		}
	}

	// try to compile the source code provided
	contracts, err := compiler.CompileSolidityString(compilerPath, sc.SourceCode)
	if err != nil {
		p.log.Errorf("solidity code compilation failed with compiler %s: %s", compilerPath, err.Error())
		return err
	}

	// loop over contracts ad try to validate one of them
	for name, detail := range contracts {
		// check if the compiled byte code match with the deployed contract
		match, err := compareContractCode(tx, detail.Code)
		if err != nil {
			p.log.Errorf("contract byte code comparison failed")
			return err
		}

		// we have the winner
		if match {
			// set the contract name if not done already
			if 0 == len(sc.Name) {
				sc.Name = strings.TrimPrefix(name, "<stdin>:")
			}

			// update the contract data
			updateContractDetails(sc, detail)

			// write update to the database
			if err := p.db.UpdateContract(sc); err != nil {
				p.log.Errorf("contract validation failed due to db error; %s", err.Error())
				return err
			}

			// inform about success
			p.log.Debugf("contract %s [%s] validated with compiler %s", sc.Address.String(), name, compilerPath)
			p.cache.EvictContract(&sc.Address)

			// inform the upper instance we have a winner
			return nil
		}
	}

	// If creation bytecode compare failed for all artifacts, try runtime bytecode compare
	// Fetch on-chain runtime code
	onChainCode, err := p.rpc.ContractCode(&sc.Address)
	if err == nil && len(onChainCode) > 0 {
		for name, detail := range contracts {
			// detail.RuntimeCode is not exposed by geth compiler output; runtime is in detail.RuntimeCode or detail.CodeRuntime depending on version
			// The geth compiler.Contract has fields: Code (creation) and RuntimeCode (runtime) in newer versions. Try both.
			runtimeHex := detail.RuntimeCode
			if len(runtimeHex) == 0 {
				// some versions use Info.RuntimeCode or RuntimeCode
				runtimeHex = detail.Info.RuntimeCode
			}
			if len(runtimeHex) == 0 {
				// as a last resort, skip runtime check for this artifact
				continue
			}

			compiledRuntime, err := hexutil.Decode(runtimeHex)
			if err != nil {
				continue
			}
			// strip metadata tail from compiled runtime as well
			compiledRuntime = cutCodeMetadata(compiledRuntime)

			// compare equality with on-chain code prefix-equality or exact? Use exact length match
			if len(onChainCode) >= len(compiledRuntime) {
				if bytes.Equal(compiledRuntime, onChainCode[:len(compiledRuntime)]) {
					// matched by runtime
					if 0 == len(sc.Name) {
						sc.Name = strings.TrimPrefix(name, "<stdin>:")
					}
					updateContractDetails(sc, detail)
					if err := p.db.UpdateContract(sc); err != nil {
						p.log.Errorf("contract validation (runtime) failed due to db error; %s", err.Error())
						return err
					}
					p.log.Debugf("contract %s [%s] validated by runtime bytecode with compiler %s", sc.Address.String(), name, compilerPath)
					p.cache.EvictContract(&sc.Address)
					return nil
				}
			}
		}
	}

	// validation fails
	return fmt.Errorf("contract source code does not match with the deployed byte code")
}

// StoreContract adds new contract into the repository.
func (p *proxy) StoreContract(con *types.Contract) error {
	// is the a known contract which will be updated?
	isUpdate := p.db.IsContractKnown(&con.Address)

	// do the add/update op
	if err := p.db.AddContract(con); err != nil {
		p.log.Errorf("contract %s store failed; %s", con.Address.String(), err.Error())
		return err
	}

	// re-scan transactions of the contract so they are up-to-date with their calls analysis
	if isUpdate {
		// log what we have done here
		p.log.Debugf("updated known contract at %s", con.Address.String())
		p.cache.EvictContract(&con.Address)
	}
	return nil
}
