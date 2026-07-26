package rpc

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// ContractCode returns the on-chain runtime bytecode of a contract at the given address.
func (nec *NecBridge) ContractCode(addr *common.Address) ([]byte, error) {
	var code string
	// Fetch code at latest block
	if err := nec.rpc.Call(&code, "eth_getCode", addr.Hex(), "latest"); err != nil {
		nec.log.Errorf("can not get code of contract [%s]", addr.Hex())
		return nil, err
	}
	return hexutil.Decode(code)
}

// StorageAt returns the raw 32-byte value stored at a given storage slot for the address.
// The slot should be provided as a 32-byte hash (padded hex string), e.g. EIP-1967 IMPLEMENTATION_SLOT.
func (nec *NecBridge) StorageAt(addr *common.Address, slot common.Hash) ([]byte, error) {
	var data string
	if err := nec.rpc.Call(&data, "eth_getStorageAt", addr.Hex(), slot.Hex(), "latest"); err != nil {
		nec.log.Errorf("can not get storage at slot %s for [%s]", slot.Hex(), addr.Hex())
		return nil, err
	}
	return hexutil.Decode(data)
}

// Call executes a read-only call on the given address with the provided input data.
func (nec *NecBridge) Call(addr *common.Address, data []byte) ([]byte, error) {
	var out string
	arg := struct {
		To   string `json:"to"`
		Data string `json:"data"`
	}{To: addr.Hex(), Data: hexutil.Encode(data)}
	if err := nec.rpc.Call(&out, "eth_call", arg, "latest"); err != nil {
		nec.log.Errorf("can not perform call on [%s]", addr.Hex())
		return nil, err
	}
	return hexutil.Decode(out)
}
