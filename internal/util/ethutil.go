package util

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/fxamacker/cbor"
)

// WordDecode holds a single 32-byte slot interpretation.
type WordDecode struct {
	Index  *int32  // GraphQL expects Int -> Go int32
	Hex    *string // hex string of the 32-byte chunk
	Uint   *string // decimal string, because GraphQL has no BigInt
	String *string // UTF-8 string, if printable
}

// GenericReturn bundles everything we can pull out without an ABI.
type GenericReturn struct {
	RevertMessages *[]string     // pointer to []string
	Panics         *[]string     // pointer to []string
	Words          *[]WordDecode // pointer to []WordDecode
	Metadata       *string       // JSON string, not map anymore
}

// DecodeReturnNoABI takes raw hex (with or without "0x") and returns GenericReturn.
func DecodeReturnNoABI(returnHex string) (*GenericReturn, error) {
	clean := strings.TrimPrefix(returnHex, "0x")
	data, err := hex.DecodeString(clean)
	if err != nil {
		return nil, err
	}

	out := &GenericReturn{}

	// 1) Revert messages: selector 0x08c379a0
	reErr := regexp.MustCompile(`08c379a0([0-9a-fA-F]+)`)
	for _, m := range reErr.FindAllStringSubmatch(clean, -1) {
		payload := m[1]
		if len(payload) > 64 {
			raw, err := hex.DecodeString(payload[64:])
			if err == nil {
				str := string(bytes.TrimRight(raw, "\x00"))
				reverts := []string{str}
				out.RevertMessages = &reverts
			}
		}
	}

	// 2) Panic codes: selector 0x4e487b71 + uint256
	rePanic := regexp.MustCompile(`4e487b71([0-9a-fA-F]{64})`)
	for _, m := range rePanic.FindAllStringSubmatch(clean, -1) {
		argHex := m[1]
		last8 := argHex[56:]
		b, err := hex.DecodeString(last8)
		if err == nil {
			var code uint64
			for _, bb := range b {
				code = (code<<8 | uint64(bb))
			}
			panics := []string{strconv.FormatUint(code, 10)}
			out.Panics = &panics
		}
	}

	// 3) Slice into 32-byte words properly
	const slot = 32
	count := len(data) / slot
	words := make([]WordDecode, 0, count)
	for i := 0; i < count; i++ {
		chunk := data[i*slot : (i+1)*slot]
		idx := int32(i)
		hexStr := "0x" + hex.EncodeToString(chunk)
		uintStr := new(big.Int).SetBytes(chunk).String()

		var strPtr *string
		if isPrintable(chunk) {
			s := string(bytes.TrimRight(chunk, "\x00"))
			strPtr = &s
		}

		words = append(words, WordDecode{
			Index:  &idx,
			Hex:    &hexStr,
			Uint:   &uintStr,
			String: strPtr,
		})
	}
	if len(words) > 0 {
		out.Words = &words
	}

	// 4) (Optional) CBOR metadata at tail, after 0xfe
	if idx := bytes.LastIndex(data, []byte{0xfe}); idx != -1 && idx+1 < len(data) {
		var meta map[string]interface{}
		if err := cbor.Unmarshal(data[idx+1:], &meta); err == nil {
			encoded, err := json.Marshal(meta)
			if err == nil {
				jsonStr := string(encoded)
				out.Metadata = &jsonStr
			}
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
