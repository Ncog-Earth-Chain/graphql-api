package util

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"regexp"
	"strings"

	"github.com/fxamacker/cbor"
)

// WordDecode holds a single 32-byte slot interpretation.
type WordDecode struct {
	Index  int      // which word (0-based)
	Hex    string   // hex string of the 32-byte chunk
	Uint   *big.Int // decimal interpretation
	String string   // UTF-8 if printable, else empty
}

// GenericReturn bundles everything we can pull out without an ABI.
type GenericReturn struct {
	RevertMessages []string               // any Error(string)
	Panics         []uint64               // any Panic(uint256)
	Words          []WordDecode           // 32-byte aligned decode
	Metadata       map[string]interface{} // CBOR tail (if any)
}

// DecodeReturnNoABI takes raw hex (with or without "0x") and returns GenericReturn.
func DecodeReturnNoABI(returnHex string) (*GenericReturn, error) {
	clean := strings.TrimPrefix(returnHex, "0x")
	data, err := hex.DecodeString(clean)
	if err != nil {
		return nil, err
	}

	out := &GenericReturn{
		Metadata: make(map[string]interface{}),
	}

	// 1) Revert messages: selector 0x08c379a0
	reErr := regexp.MustCompile(`08c379a0([0-9a-fA-F]+)`)
	for _, m := range reErr.FindAllStringSubmatch(clean, -1) {
		payload := m[1]
		if len(payload) > 64 {
			// skip the 32-byte offset+length header
			raw, err := hex.DecodeString(payload[64:])
			if err == nil {
				// trim trailing zeros
				str := string(bytes.TrimRight(raw, "\x00"))
				out.RevertMessages = append(out.RevertMessages, str)
			}
		}
	}

	// 2) Panic codes: selector 0x4e487b71 + uint256
	rePanic := regexp.MustCompile(`4e487b71([0-9a-fA-F]{64})`)
	for _, m := range rePanic.FindAllStringSubmatch(clean, -1) {
		argHex := m[1]       // full 32-byte
		last8 := argHex[56:] // last 8 hex chars = last 4 bytes
		b, err := hex.DecodeString(last8)
		if err == nil {
			var code uint64
			for _, bb := range b {
				code = (code<<8 | uint64(bb))
			}
			out.Panics = append(out.Panics, code)
		}
	}

	// 3) Slice into 32-byte words
	const slot = 32
	count := len(data) / slot
	for i := 0; i < count; i++ {
		chunk := data[i*slot : (i+1)*slot]
		wi := WordDecode{
			Index: i,
			Hex:   "0x" + hex.EncodeToString(chunk),
			Uint:  new(big.Int).SetBytes(chunk),
		}
		// if printable ASCII, capture string
		if isPrintable(chunk) {
			wi.String = string(bytes.TrimRight(chunk, "\x00"))
		}
		out.Words = append(out.Words, wi)
	}

	// 4) (Optional) CBOR metadata at tail, after 0xfe
	if idx := bytes.LastIndex(data, []byte{0xfe}); idx != -1 && idx+1 < len(data) {
		var meta map[string]interface{}
		if err := cbor.Unmarshal(data[idx+1:], &meta); err == nil {
			out.Metadata = meta
		}
	}

	return out, nil
}

// isPrintable returns true if all bytes are in the 32–126 ASCII range or newline.
func isPrintable(b []byte) bool {
	for _, c := range b {
		if (c >= 32 && c <= 126) || c == 10 || c == 13 {
			continue
		}
		return false
	}
	return true
}
