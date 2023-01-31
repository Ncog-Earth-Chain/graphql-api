// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package contracts

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
)

// ErcWrappedNecMetaData contains all meta data concerning the ErcWrappedNec contract.
var ErcWrappedNecMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Approval\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"Paused\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"PauserAdded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"PauserRemoved\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Transfer\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"Unpaused\",\"type\":\"event\"},{\"constant\":true,\"inputs\":[],\"name\":\"ERR_INVALID_ZERO_VALUE\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"ERR_NO_ERROR\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"addPauser\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"}],\"name\":\"allowance\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"approve\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"balanceOf\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"decimals\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"subtractedValue\",\"type\":\"uint256\"}],\"name\":\"decreaseAllowance\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"addedValue\",\"type\":\"uint256\"}],\"name\":\"increaseAllowance\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"isPauser\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"name\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[],\"name\":\"pause\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"paused\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[],\"name\":\"renouncePauser\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"symbol\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":true,\"inputs\":[],\"name\":\"totalSupply\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"view\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"transfer\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"transferFrom\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[],\"name\":\"unpause\",\"outputs\":[],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[],\"name\":\"deposit\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":true,\"stateMutability\":\"payable\",\"type\":\"function\"},{\"constant\":false,\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"withdraw\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"payable\":false,\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// ErcWrappedNecABI is the input ABI used to generate the binding from.
// Deprecated: Use ErcWrappedNecMetaData.ABI instead.
var ErcWrappedNecABI = ErcWrappedNecMetaData.ABI

// ErcWrappedNec is an auto generated Go binding around an Ethereum contract.
type ErcWrappedNec struct {
	ErcWrappedNecCaller     // Read-only binding to the contract
	ErcWrappedNecTransactor // Write-only binding to the contract
	ErcWrappedNecFilterer   // Log filterer for contract events
}

// ErcWrappedNecCaller is an auto generated read-only Go binding around an Ethereum contract.
type ErcWrappedNecCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ErcWrappedNecTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ErcWrappedNecTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ErcWrappedNecFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ErcWrappedNecFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ErcWrappedNecSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ErcWrappedNecSession struct {
	Contract     *ErcWrappedNec    // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ErcWrappedNecCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ErcWrappedNecCallerSession struct {
	Contract *ErcWrappedNecCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts        // Call options to use throughout this session
}

// ErcWrappedNecTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ErcWrappedNecTransactorSession struct {
	Contract     *ErcWrappedNecTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts        // Transaction auth options to use throughout this session
}

// ErcWrappedNecRaw is an auto generated low-level Go binding around an Ethereum contract.
type ErcWrappedNecRaw struct {
	Contract *ErcWrappedNec // Generic contract binding to access the raw methods on
}

// ErcWrappedNecCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ErcWrappedNecCallerRaw struct {
	Contract *ErcWrappedNecCaller // Generic read-only contract binding to access the raw methods on
}

// ErcWrappedNecTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ErcWrappedNecTransactorRaw struct {
	Contract *ErcWrappedNecTransactor // Generic write-only contract binding to access the raw methods on
}

// NewErcWrappedNec creates a new instance of ErcWrappedNec, bound to a specific deployed contract.
func NewErcWrappedNec(address common.Address, backend bind.ContractBackend) (*ErcWrappedNec, error) {
	contract, err := bindErcWrappedNec(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ErcWrappedNec{ErcWrappedNecCaller: ErcWrappedNecCaller{contract: contract}, ErcWrappedNecTransactor: ErcWrappedNecTransactor{contract: contract}, ErcWrappedNecFilterer: ErcWrappedNecFilterer{contract: contract}}, nil
}

// NewErcWrappedNecCaller creates a new read-only instance of ErcWrappedNec, bound to a specific deployed contract.
func NewErcWrappedNecCaller(address common.Address, caller bind.ContractCaller) (*ErcWrappedNecCaller, error) {
	contract, err := bindErcWrappedNec(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ErcWrappedNecCaller{contract: contract}, nil
}

// NewErcWrappedNecTransactor creates a new write-only instance of ErcWrappedNec, bound to a specific deployed contract.
func NewErcWrappedNecTransactor(address common.Address, transactor bind.ContractTransactor) (*ErcWrappedNecTransactor, error) {
	contract, err := bindErcWrappedNec(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ErcWrappedNecTransactor{contract: contract}, nil
}

// NewErcWrappedNecFilterer creates a new log filterer instance of ErcWrappedNec, bound to a specific deployed contract.
func NewErcWrappedNecFilterer(address common.Address, filterer bind.ContractFilterer) (*ErcWrappedNecFilterer, error) {
	contract, err := bindErcWrappedNec(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ErcWrappedNecFilterer{contract: contract}, nil
}

// bindErcWrappedNec binds a generic wrapper to an already deployed contract.
func bindErcWrappedNec(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := abi.JSON(strings.NewReader(ErcWrappedNecABI))
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ErcWrappedNec *ErcWrappedNecRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ErcWrappedNec.Contract.ErcWrappedNecCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ErcWrappedNec *ErcWrappedNecRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.ErcWrappedNecTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ErcWrappedNec *ErcWrappedNecRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.ErcWrappedNecTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ErcWrappedNec *ErcWrappedNecCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ErcWrappedNec.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ErcWrappedNec *ErcWrappedNecTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ErcWrappedNec *ErcWrappedNecTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.contract.Transact(opts, method, params...)
}

// ERRINVALIDZEROVALUE is a free data retrieval call binding the contract method 0x6d7497b3.
//
// Solidity: function ERR_INVALID_ZERO_VALUE() view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecCaller) ERRINVALIDZEROVALUE(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ErcWrappedNec.contract.Call(opts, &out, "ERR_INVALID_ZERO_VALUE")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ERRINVALIDZEROVALUE is a free data retrieval call binding the contract method 0x6d7497b3.
//
// Solidity: function ERR_INVALID_ZERO_VALUE() view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecSession) ERRINVALIDZEROVALUE() (*big.Int, error) {
	return _ErcWrappedNec.Contract.ERRINVALIDZEROVALUE(&_ErcWrappedNec.CallOpts)
}

// ERRINVALIDZEROVALUE is a free data retrieval call binding the contract method 0x6d7497b3.
//
// Solidity: function ERR_INVALID_ZERO_VALUE() view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecCallerSession) ERRINVALIDZEROVALUE() (*big.Int, error) {
	return _ErcWrappedNec.Contract.ERRINVALIDZEROVALUE(&_ErcWrappedNec.CallOpts)
}

// ERRNOERROR is a free data retrieval call binding the contract method 0x35052d6e.
//
// Solidity: function ERR_NO_ERROR() view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecCaller) ERRNOERROR(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ErcWrappedNec.contract.Call(opts, &out, "ERR_NO_ERROR")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ERRNOERROR is a free data retrieval call binding the contract method 0x35052d6e.
//
// Solidity: function ERR_NO_ERROR() view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecSession) ERRNOERROR() (*big.Int, error) {
	return _ErcWrappedNec.Contract.ERRNOERROR(&_ErcWrappedNec.CallOpts)
}

// ERRNOERROR is a free data retrieval call binding the contract method 0x35052d6e.
//
// Solidity: function ERR_NO_ERROR() view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecCallerSession) ERRNOERROR() (*big.Int, error) {
	return _ErcWrappedNec.Contract.ERRNOERROR(&_ErcWrappedNec.CallOpts)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecCaller) Allowance(opts *bind.CallOpts, owner common.Address, spender common.Address) (*big.Int, error) {
	var out []interface{}
	err := _ErcWrappedNec.contract.Call(opts, &out, "allowance", owner, spender)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecSession) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _ErcWrappedNec.Contract.Allowance(&_ErcWrappedNec.CallOpts, owner, spender)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecCallerSession) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _ErcWrappedNec.Contract.Allowance(&_ErcWrappedNec.CallOpts, owner, spender)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecCaller) BalanceOf(opts *bind.CallOpts, account common.Address) (*big.Int, error) {
	var out []interface{}
	err := _ErcWrappedNec.contract.Call(opts, &out, "balanceOf", account)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecSession) BalanceOf(account common.Address) (*big.Int, error) {
	return _ErcWrappedNec.Contract.BalanceOf(&_ErcWrappedNec.CallOpts, account)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecCallerSession) BalanceOf(account common.Address) (*big.Int, error) {
	return _ErcWrappedNec.Contract.BalanceOf(&_ErcWrappedNec.CallOpts, account)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_ErcWrappedNec *ErcWrappedNecCaller) Decimals(opts *bind.CallOpts) (uint8, error) {
	var out []interface{}
	err := _ErcWrappedNec.contract.Call(opts, &out, "decimals")

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_ErcWrappedNec *ErcWrappedNecSession) Decimals() (uint8, error) {
	return _ErcWrappedNec.Contract.Decimals(&_ErcWrappedNec.CallOpts)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_ErcWrappedNec *ErcWrappedNecCallerSession) Decimals() (uint8, error) {
	return _ErcWrappedNec.Contract.Decimals(&_ErcWrappedNec.CallOpts)
}

// IsPauser is a free data retrieval call binding the contract method 0x46fbf68e.
//
// Solidity: function isPauser(address account) view returns(bool)
func (_ErcWrappedNec *ErcWrappedNecCaller) IsPauser(opts *bind.CallOpts, account common.Address) (bool, error) {
	var out []interface{}
	err := _ErcWrappedNec.contract.Call(opts, &out, "isPauser", account)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsPauser is a free data retrieval call binding the contract method 0x46fbf68e.
//
// Solidity: function isPauser(address account) view returns(bool)
func (_ErcWrappedNec *ErcWrappedNecSession) IsPauser(account common.Address) (bool, error) {
	return _ErcWrappedNec.Contract.IsPauser(&_ErcWrappedNec.CallOpts, account)
}

// IsPauser is a free data retrieval call binding the contract method 0x46fbf68e.
//
// Solidity: function isPauser(address account) view returns(bool)
func (_ErcWrappedNec *ErcWrappedNecCallerSession) IsPauser(account common.Address) (bool, error) {
	return _ErcWrappedNec.Contract.IsPauser(&_ErcWrappedNec.CallOpts, account)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_ErcWrappedNec *ErcWrappedNecCaller) Name(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _ErcWrappedNec.contract.Call(opts, &out, "name")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_ErcWrappedNec *ErcWrappedNecSession) Name() (string, error) {
	return _ErcWrappedNec.Contract.Name(&_ErcWrappedNec.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_ErcWrappedNec *ErcWrappedNecCallerSession) Name() (string, error) {
	return _ErcWrappedNec.Contract.Name(&_ErcWrappedNec.CallOpts)
}

// Paused is a free data retrieval call binding the contract method 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (_ErcWrappedNec *ErcWrappedNecCaller) Paused(opts *bind.CallOpts) (bool, error) {
	var out []interface{}
	err := _ErcWrappedNec.contract.Call(opts, &out, "paused")

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// Paused is a free data retrieval call binding the contract method 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (_ErcWrappedNec *ErcWrappedNecSession) Paused() (bool, error) {
	return _ErcWrappedNec.Contract.Paused(&_ErcWrappedNec.CallOpts)
}

// Paused is a free data retrieval call binding the contract method 0x5c975abb.
//
// Solidity: function paused() view returns(bool)
func (_ErcWrappedNec *ErcWrappedNecCallerSession) Paused() (bool, error) {
	return _ErcWrappedNec.Contract.Paused(&_ErcWrappedNec.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_ErcWrappedNec *ErcWrappedNecCaller) Symbol(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _ErcWrappedNec.contract.Call(opts, &out, "symbol")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_ErcWrappedNec *ErcWrappedNecSession) Symbol() (string, error) {
	return _ErcWrappedNec.Contract.Symbol(&_ErcWrappedNec.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_ErcWrappedNec *ErcWrappedNecCallerSession) Symbol() (string, error) {
	return _ErcWrappedNec.Contract.Symbol(&_ErcWrappedNec.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecCaller) TotalSupply(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ErcWrappedNec.contract.Call(opts, &out, "totalSupply")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecSession) TotalSupply() (*big.Int, error) {
	return _ErcWrappedNec.Contract.TotalSupply(&_ErcWrappedNec.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecCallerSession) TotalSupply() (*big.Int, error) {
	return _ErcWrappedNec.Contract.TotalSupply(&_ErcWrappedNec.CallOpts)
}

// AddPauser is a paid mutator transaction binding the contract method 0x82dc1ec4.
//
// Solidity: function addPauser(address account) returns()
func (_ErcWrappedNec *ErcWrappedNecTransactor) AddPauser(opts *bind.TransactOpts, account common.Address) (*types.Transaction, error) {
	return _ErcWrappedNec.contract.Transact(opts, "addPauser", account)
}

// AddPauser is a paid mutator transaction binding the contract method 0x82dc1ec4.
//
// Solidity: function addPauser(address account) returns()
func (_ErcWrappedNec *ErcWrappedNecSession) AddPauser(account common.Address) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.AddPauser(&_ErcWrappedNec.TransactOpts, account)
}

// AddPauser is a paid mutator transaction binding the contract method 0x82dc1ec4.
//
// Solidity: function addPauser(address account) returns()
func (_ErcWrappedNec *ErcWrappedNecTransactorSession) AddPauser(account common.Address) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.AddPauser(&_ErcWrappedNec.TransactOpts, account)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecTransactor) Approve(opts *bind.TransactOpts, spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.contract.Transact(opts, "approve", spender, value)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecSession) Approve(spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.Approve(&_ErcWrappedNec.TransactOpts, spender, value)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 value) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecTransactorSession) Approve(spender common.Address, value *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.Approve(&_ErcWrappedNec.TransactOpts, spender, value)
}

// DecreaseAllowance is a paid mutator transaction binding the contract method 0xa457c2d7.
//
// Solidity: function decreaseAllowance(address spender, uint256 subtractedValue) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecTransactor) DecreaseAllowance(opts *bind.TransactOpts, spender common.Address, subtractedValue *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.contract.Transact(opts, "decreaseAllowance", spender, subtractedValue)
}

// DecreaseAllowance is a paid mutator transaction binding the contract method 0xa457c2d7.
//
// Solidity: function decreaseAllowance(address spender, uint256 subtractedValue) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecSession) DecreaseAllowance(spender common.Address, subtractedValue *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.DecreaseAllowance(&_ErcWrappedNec.TransactOpts, spender, subtractedValue)
}

// DecreaseAllowance is a paid mutator transaction binding the contract method 0xa457c2d7.
//
// Solidity: function decreaseAllowance(address spender, uint256 subtractedValue) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecTransactorSession) DecreaseAllowance(spender common.Address, subtractedValue *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.DecreaseAllowance(&_ErcWrappedNec.TransactOpts, spender, subtractedValue)
}

// Deposit is a paid mutator transaction binding the contract method 0xd0e30db0.
//
// Solidity: function deposit() payable returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecTransactor) Deposit(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ErcWrappedNec.contract.Transact(opts, "deposit")
}

// Deposit is a paid mutator transaction binding the contract method 0xd0e30db0.
//
// Solidity: function deposit() payable returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecSession) Deposit() (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.Deposit(&_ErcWrappedNec.TransactOpts)
}

// Deposit is a paid mutator transaction binding the contract method 0xd0e30db0.
//
// Solidity: function deposit() payable returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecTransactorSession) Deposit() (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.Deposit(&_ErcWrappedNec.TransactOpts)
}

// IncreaseAllowance is a paid mutator transaction binding the contract method 0x39509351.
//
// Solidity: function increaseAllowance(address spender, uint256 addedValue) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecTransactor) IncreaseAllowance(opts *bind.TransactOpts, spender common.Address, addedValue *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.contract.Transact(opts, "increaseAllowance", spender, addedValue)
}

// IncreaseAllowance is a paid mutator transaction binding the contract method 0x39509351.
//
// Solidity: function increaseAllowance(address spender, uint256 addedValue) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecSession) IncreaseAllowance(spender common.Address, addedValue *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.IncreaseAllowance(&_ErcWrappedNec.TransactOpts, spender, addedValue)
}

// IncreaseAllowance is a paid mutator transaction binding the contract method 0x39509351.
//
// Solidity: function increaseAllowance(address spender, uint256 addedValue) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecTransactorSession) IncreaseAllowance(spender common.Address, addedValue *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.IncreaseAllowance(&_ErcWrappedNec.TransactOpts, spender, addedValue)
}

// Pause is a paid mutator transaction binding the contract method 0x8456cb59.
//
// Solidity: function pause() returns()
func (_ErcWrappedNec *ErcWrappedNecTransactor) Pause(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ErcWrappedNec.contract.Transact(opts, "pause")
}

// Pause is a paid mutator transaction binding the contract method 0x8456cb59.
//
// Solidity: function pause() returns()
func (_ErcWrappedNec *ErcWrappedNecSession) Pause() (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.Pause(&_ErcWrappedNec.TransactOpts)
}

// Pause is a paid mutator transaction binding the contract method 0x8456cb59.
//
// Solidity: function pause() returns()
func (_ErcWrappedNec *ErcWrappedNecTransactorSession) Pause() (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.Pause(&_ErcWrappedNec.TransactOpts)
}

// RenouncePauser is a paid mutator transaction binding the contract method 0x6ef8d66d.
//
// Solidity: function renouncePauser() returns()
func (_ErcWrappedNec *ErcWrappedNecTransactor) RenouncePauser(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ErcWrappedNec.contract.Transact(opts, "renouncePauser")
}

// RenouncePauser is a paid mutator transaction binding the contract method 0x6ef8d66d.
//
// Solidity: function renouncePauser() returns()
func (_ErcWrappedNec *ErcWrappedNecSession) RenouncePauser() (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.RenouncePauser(&_ErcWrappedNec.TransactOpts)
}

// RenouncePauser is a paid mutator transaction binding the contract method 0x6ef8d66d.
//
// Solidity: function renouncePauser() returns()
func (_ErcWrappedNec *ErcWrappedNecTransactorSession) RenouncePauser() (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.RenouncePauser(&_ErcWrappedNec.TransactOpts)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecTransactor) Transfer(opts *bind.TransactOpts, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.contract.Transact(opts, "transfer", to, value)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecSession) Transfer(to common.Address, value *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.Transfer(&_ErcWrappedNec.TransactOpts, to, value)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 value) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecTransactorSession) Transfer(to common.Address, value *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.Transfer(&_ErcWrappedNec.TransactOpts, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecTransactor) TransferFrom(opts *bind.TransactOpts, from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.contract.Transact(opts, "transferFrom", from, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecSession) TransferFrom(from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.TransferFrom(&_ErcWrappedNec.TransactOpts, from, to, value)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 value) returns(bool)
func (_ErcWrappedNec *ErcWrappedNecTransactorSession) TransferFrom(from common.Address, to common.Address, value *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.TransferFrom(&_ErcWrappedNec.TransactOpts, from, to, value)
}

// Unpause is a paid mutator transaction binding the contract method 0x3f4ba83a.
//
// Solidity: function unpause() returns()
func (_ErcWrappedNec *ErcWrappedNecTransactor) Unpause(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ErcWrappedNec.contract.Transact(opts, "unpause")
}

// Unpause is a paid mutator transaction binding the contract method 0x3f4ba83a.
//
// Solidity: function unpause() returns()
func (_ErcWrappedNec *ErcWrappedNecSession) Unpause() (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.Unpause(&_ErcWrappedNec.TransactOpts)
}

// Unpause is a paid mutator transaction binding the contract method 0x3f4ba83a.
//
// Solidity: function unpause() returns()
func (_ErcWrappedNec *ErcWrappedNecTransactorSession) Unpause() (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.Unpause(&_ErcWrappedNec.TransactOpts)
}

// Withdraw is a paid mutator transaction binding the contract method 0x2e1a7d4d.
//
// Solidity: function withdraw(uint256 amount) returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecTransactor) Withdraw(opts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.contract.Transact(opts, "withdraw", amount)
}

// Withdraw is a paid mutator transaction binding the contract method 0x2e1a7d4d.
//
// Solidity: function withdraw(uint256 amount) returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecSession) Withdraw(amount *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.Withdraw(&_ErcWrappedNec.TransactOpts, amount)
}

// Withdraw is a paid mutator transaction binding the contract method 0x2e1a7d4d.
//
// Solidity: function withdraw(uint256 amount) returns(uint256)
func (_ErcWrappedNec *ErcWrappedNecTransactorSession) Withdraw(amount *big.Int) (*types.Transaction, error) {
	return _ErcWrappedNec.Contract.Withdraw(&_ErcWrappedNec.TransactOpts, amount)
}

// ErcWrappedNecApprovalIterator is returned from FilterApproval and is used to iterate over the raw logs and unpacked data for Approval events raised by the ErcWrappedNec contract.
type ErcWrappedNecApprovalIterator struct {
	Event *ErcWrappedNecApproval // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ErcWrappedNecApprovalIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ErcWrappedNecApproval)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ErcWrappedNecApproval)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ErcWrappedNecApprovalIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ErcWrappedNecApprovalIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ErcWrappedNecApproval represents a Approval event raised by the ErcWrappedNec contract.
type ErcWrappedNecApproval struct {
	Owner   common.Address
	Spender common.Address
	Value   *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterApproval is a free log retrieval operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_ErcWrappedNec *ErcWrappedNecFilterer) FilterApproval(opts *bind.FilterOpts, owner []common.Address, spender []common.Address) (*ErcWrappedNecApprovalIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _ErcWrappedNec.contract.FilterLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return &ErcWrappedNecApprovalIterator{contract: _ErcWrappedNec.contract, event: "Approval", logs: logs, sub: sub}, nil
}

// WatchApproval is a free log subscription operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_ErcWrappedNec *ErcWrappedNecFilterer) WatchApproval(opts *bind.WatchOpts, sink chan<- *ErcWrappedNecApproval, owner []common.Address, spender []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _ErcWrappedNec.contract.WatchLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ErcWrappedNecApproval)
				if err := _ErcWrappedNec.contract.UnpackLog(event, "Approval", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseApproval is a log parse operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_ErcWrappedNec *ErcWrappedNecFilterer) ParseApproval(log types.Log) (*ErcWrappedNecApproval, error) {
	event := new(ErcWrappedNecApproval)
	if err := _ErcWrappedNec.contract.UnpackLog(event, "Approval", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ErcWrappedNecPausedIterator is returned from FilterPaused and is used to iterate over the raw logs and unpacked data for Paused events raised by the ErcWrappedNec contract.
type ErcWrappedNecPausedIterator struct {
	Event *ErcWrappedNecPaused // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ErcWrappedNecPausedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ErcWrappedNecPaused)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ErcWrappedNecPaused)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ErcWrappedNecPausedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ErcWrappedNecPausedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ErcWrappedNecPaused represents a Paused event raised by the ErcWrappedNec contract.
type ErcWrappedNecPaused struct {
	Account common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterPaused is a free log retrieval operation binding the contract event 0x62e78cea01bee320cd4e420270b5ea74000d11b0c9f74754ebdbfc544b05a258.
//
// Solidity: event Paused(address account)
func (_ErcWrappedNec *ErcWrappedNecFilterer) FilterPaused(opts *bind.FilterOpts) (*ErcWrappedNecPausedIterator, error) {

	logs, sub, err := _ErcWrappedNec.contract.FilterLogs(opts, "Paused")
	if err != nil {
		return nil, err
	}
	return &ErcWrappedNecPausedIterator{contract: _ErcWrappedNec.contract, event: "Paused", logs: logs, sub: sub}, nil
}

// WatchPaused is a free log subscription operation binding the contract event 0x62e78cea01bee320cd4e420270b5ea74000d11b0c9f74754ebdbfc544b05a258.
//
// Solidity: event Paused(address account)
func (_ErcWrappedNec *ErcWrappedNecFilterer) WatchPaused(opts *bind.WatchOpts, sink chan<- *ErcWrappedNecPaused) (event.Subscription, error) {

	logs, sub, err := _ErcWrappedNec.contract.WatchLogs(opts, "Paused")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ErcWrappedNecPaused)
				if err := _ErcWrappedNec.contract.UnpackLog(event, "Paused", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParsePaused is a log parse operation binding the contract event 0x62e78cea01bee320cd4e420270b5ea74000d11b0c9f74754ebdbfc544b05a258.
//
// Solidity: event Paused(address account)
func (_ErcWrappedNec *ErcWrappedNecFilterer) ParsePaused(log types.Log) (*ErcWrappedNecPaused, error) {
	event := new(ErcWrappedNecPaused)
	if err := _ErcWrappedNec.contract.UnpackLog(event, "Paused", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ErcWrappedNecPauserAddedIterator is returned from FilterPauserAdded and is used to iterate over the raw logs and unpacked data for PauserAdded events raised by the ErcWrappedNec contract.
type ErcWrappedNecPauserAddedIterator struct {
	Event *ErcWrappedNecPauserAdded // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ErcWrappedNecPauserAddedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ErcWrappedNecPauserAdded)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ErcWrappedNecPauserAdded)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ErcWrappedNecPauserAddedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ErcWrappedNecPauserAddedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ErcWrappedNecPauserAdded represents a PauserAdded event raised by the ErcWrappedNec contract.
type ErcWrappedNecPauserAdded struct {
	Account common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterPauserAdded is a free log retrieval operation binding the contract event 0x6719d08c1888103bea251a4ed56406bd0c3e69723c8a1686e017e7bbe159b6f8.
//
// Solidity: event PauserAdded(address indexed account)
func (_ErcWrappedNec *ErcWrappedNecFilterer) FilterPauserAdded(opts *bind.FilterOpts, account []common.Address) (*ErcWrappedNecPauserAddedIterator, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _ErcWrappedNec.contract.FilterLogs(opts, "PauserAdded", accountRule)
	if err != nil {
		return nil, err
	}
	return &ErcWrappedNecPauserAddedIterator{contract: _ErcWrappedNec.contract, event: "PauserAdded", logs: logs, sub: sub}, nil
}

// WatchPauserAdded is a free log subscription operation binding the contract event 0x6719d08c1888103bea251a4ed56406bd0c3e69723c8a1686e017e7bbe159b6f8.
//
// Solidity: event PauserAdded(address indexed account)
func (_ErcWrappedNec *ErcWrappedNecFilterer) WatchPauserAdded(opts *bind.WatchOpts, sink chan<- *ErcWrappedNecPauserAdded, account []common.Address) (event.Subscription, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _ErcWrappedNec.contract.WatchLogs(opts, "PauserAdded", accountRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ErcWrappedNecPauserAdded)
				if err := _ErcWrappedNec.contract.UnpackLog(event, "PauserAdded", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParsePauserAdded is a log parse operation binding the contract event 0x6719d08c1888103bea251a4ed56406bd0c3e69723c8a1686e017e7bbe159b6f8.
//
// Solidity: event PauserAdded(address indexed account)
func (_ErcWrappedNec *ErcWrappedNecFilterer) ParsePauserAdded(log types.Log) (*ErcWrappedNecPauserAdded, error) {
	event := new(ErcWrappedNecPauserAdded)
	if err := _ErcWrappedNec.contract.UnpackLog(event, "PauserAdded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ErcWrappedNecPauserRemovedIterator is returned from FilterPauserRemoved and is used to iterate over the raw logs and unpacked data for PauserRemoved events raised by the ErcWrappedNec contract.
type ErcWrappedNecPauserRemovedIterator struct {
	Event *ErcWrappedNecPauserRemoved // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ErcWrappedNecPauserRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ErcWrappedNecPauserRemoved)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ErcWrappedNecPauserRemoved)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ErcWrappedNecPauserRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ErcWrappedNecPauserRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ErcWrappedNecPauserRemoved represents a PauserRemoved event raised by the ErcWrappedNec contract.
type ErcWrappedNecPauserRemoved struct {
	Account common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterPauserRemoved is a free log retrieval operation binding the contract event 0xcd265ebaf09df2871cc7bd4133404a235ba12eff2041bb89d9c714a2621c7c7e.
//
// Solidity: event PauserRemoved(address indexed account)
func (_ErcWrappedNec *ErcWrappedNecFilterer) FilterPauserRemoved(opts *bind.FilterOpts, account []common.Address) (*ErcWrappedNecPauserRemovedIterator, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _ErcWrappedNec.contract.FilterLogs(opts, "PauserRemoved", accountRule)
	if err != nil {
		return nil, err
	}
	return &ErcWrappedNecPauserRemovedIterator{contract: _ErcWrappedNec.contract, event: "PauserRemoved", logs: logs, sub: sub}, nil
}

// WatchPauserRemoved is a free log subscription operation binding the contract event 0xcd265ebaf09df2871cc7bd4133404a235ba12eff2041bb89d9c714a2621c7c7e.
//
// Solidity: event PauserRemoved(address indexed account)
func (_ErcWrappedNec *ErcWrappedNecFilterer) WatchPauserRemoved(opts *bind.WatchOpts, sink chan<- *ErcWrappedNecPauserRemoved, account []common.Address) (event.Subscription, error) {

	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}

	logs, sub, err := _ErcWrappedNec.contract.WatchLogs(opts, "PauserRemoved", accountRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ErcWrappedNecPauserRemoved)
				if err := _ErcWrappedNec.contract.UnpackLog(event, "PauserRemoved", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParsePauserRemoved is a log parse operation binding the contract event 0xcd265ebaf09df2871cc7bd4133404a235ba12eff2041bb89d9c714a2621c7c7e.
//
// Solidity: event PauserRemoved(address indexed account)
func (_ErcWrappedNec *ErcWrappedNecFilterer) ParsePauserRemoved(log types.Log) (*ErcWrappedNecPauserRemoved, error) {
	event := new(ErcWrappedNecPauserRemoved)
	if err := _ErcWrappedNec.contract.UnpackLog(event, "PauserRemoved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ErcWrappedNecTransferIterator is returned from FilterTransfer and is used to iterate over the raw logs and unpacked data for Transfer events raised by the ErcWrappedNec contract.
type ErcWrappedNecTransferIterator struct {
	Event *ErcWrappedNecTransfer // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ErcWrappedNecTransferIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ErcWrappedNecTransfer)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ErcWrappedNecTransfer)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ErcWrappedNecTransferIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ErcWrappedNecTransferIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ErcWrappedNecTransfer represents a Transfer event raised by the ErcWrappedNec contract.
type ErcWrappedNecTransfer struct {
	From  common.Address
	To    common.Address
	Value *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterTransfer is a free log retrieval operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_ErcWrappedNec *ErcWrappedNecFilterer) FilterTransfer(opts *bind.FilterOpts, from []common.Address, to []common.Address) (*ErcWrappedNecTransferIterator, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _ErcWrappedNec.contract.FilterLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return &ErcWrappedNecTransferIterator{contract: _ErcWrappedNec.contract, event: "Transfer", logs: logs, sub: sub}, nil
}

// WatchTransfer is a free log subscription operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_ErcWrappedNec *ErcWrappedNecFilterer) WatchTransfer(opts *bind.WatchOpts, sink chan<- *ErcWrappedNecTransfer, from []common.Address, to []common.Address) (event.Subscription, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _ErcWrappedNec.contract.WatchLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ErcWrappedNecTransfer)
				if err := _ErcWrappedNec.contract.UnpackLog(event, "Transfer", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTransfer is a log parse operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_ErcWrappedNec *ErcWrappedNecFilterer) ParseTransfer(log types.Log) (*ErcWrappedNecTransfer, error) {
	event := new(ErcWrappedNecTransfer)
	if err := _ErcWrappedNec.contract.UnpackLog(event, "Transfer", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ErcWrappedNecUnpausedIterator is returned from FilterUnpaused and is used to iterate over the raw logs and unpacked data for Unpaused events raised by the ErcWrappedNec contract.
type ErcWrappedNecUnpausedIterator struct {
	Event *ErcWrappedNecUnpaused // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ErcWrappedNecUnpausedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ErcWrappedNecUnpaused)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ErcWrappedNecUnpaused)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ErcWrappedNecUnpausedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ErcWrappedNecUnpausedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ErcWrappedNecUnpaused represents a Unpaused event raised by the ErcWrappedNec contract.
type ErcWrappedNecUnpaused struct {
	Account common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterUnpaused is a free log retrieval operation binding the contract event 0x5db9ee0a495bf2e6ff9c91a7834c1ba4fdd244a5e8aa4e537bd38aeae4b073aa.
//
// Solidity: event Unpaused(address account)
func (_ErcWrappedNec *ErcWrappedNecFilterer) FilterUnpaused(opts *bind.FilterOpts) (*ErcWrappedNecUnpausedIterator, error) {

	logs, sub, err := _ErcWrappedNec.contract.FilterLogs(opts, "Unpaused")
	if err != nil {
		return nil, err
	}
	return &ErcWrappedNecUnpausedIterator{contract: _ErcWrappedNec.contract, event: "Unpaused", logs: logs, sub: sub}, nil
}

// WatchUnpaused is a free log subscription operation binding the contract event 0x5db9ee0a495bf2e6ff9c91a7834c1ba4fdd244a5e8aa4e537bd38aeae4b073aa.
//
// Solidity: event Unpaused(address account)
func (_ErcWrappedNec *ErcWrappedNecFilterer) WatchUnpaused(opts *bind.WatchOpts, sink chan<- *ErcWrappedNecUnpaused) (event.Subscription, error) {

	logs, sub, err := _ErcWrappedNec.contract.WatchLogs(opts, "Unpaused")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ErcWrappedNecUnpaused)
				if err := _ErcWrappedNec.contract.UnpackLog(event, "Unpaused", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseUnpaused is a log parse operation binding the contract event 0x5db9ee0a495bf2e6ff9c91a7834c1ba4fdd244a5e8aa4e537bd38aeae4b073aa.
//
// Solidity: event Unpaused(address account)
func (_ErcWrappedNec *ErcWrappedNecFilterer) ParseUnpaused(log types.Log) (*ErcWrappedNecUnpaused, error) {
	event := new(ErcWrappedNecUnpaused)
	if err := _ErcWrappedNec.contract.UnpackLog(event, "Unpaused", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
