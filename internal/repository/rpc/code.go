package rpc

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// ContractCode returns the on-chain runtime bytecode of a contract at the given address.
func (nec *NecBridge) ContractCode(addr *common.Address) ([]byte, error) {
	var code string
	// Fetch code at latest block
	if err := nec.rpc.Call(&code, "nec_getCode", addr.Hex(), "latest"); err != nil {
		nec.log.Errorf("can not get code of contract [%s]", addr.Hex())
		return nil, err
	}
	return hexutil.Decode(code)
}
