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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"ncogearthchain-api-graphql/internal/types"
	"net/http"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Contract extract a smart contract information by account address, if available.
func (p *proxy) Contract(addr *common.Address) (*types.Contract, error) {
	// try cache first
	sc := p.cache.PullContract(addr)

	// we still don't know the contract? call the db for that
	if sc == nil {
		var err error
		sc, err = p.pg.Contract(storeCtx(), addr)
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
	rows, err := p.pg.Contracts(storeCtx(), validatedOnly, derefCursor(cursor), count)
	if err != nil {
		return nil, err
	}
	total, err := p.pg.ContractCount(storeCtx(), validatedOnly)
	if err != nil {
		return nil, err
	}
	return buildContractList(rows, total, count), nil
}

// cutCodeMetadata removes the IPFS/Swarm metadata information from the code
// for partial comparison. The current version of the Solidity compiler usually
// adds metadata to the end of the deployed byte code.
// @see https://solidity.readthedocs.io/en/latest/metadata.html
func cutCodeMetadata(bc []byte) []byte {
	// last 2 bytes are expected to contain metadata length
	bcLen := uint64(len(bc))
	if bcLen < 2 {
		return bc
	}
	cut := uint64(bc[bcLen-2])<<8 | uint64(bc[bcLen-1])

	// are we safely within the byte code size?
	if cut == 0 || cut >= bcLen-2 {
		return bc
	}

	return bc[:bcLen-cut-2]
}

// compiledArtifact represents a minimal subset of compiler output we need
type compiledArtifact struct {
	Name                 string
	Code                 string // creation bytecode hex (0x...)
	RuntimeCode          string // deployed/runtime bytecode hex (0x...)
	Abi                  json.RawMessage
	Metadata             string
	CompilerVersion      string
	CreationLinkRefs     []linkPos
	RuntimeLinkRefs      []linkPos
	RuntimeImmutableRefs []linkPos
}

// importRegexp matches Solidity import statements and extracts the path inside quotes.
var importRegexp = regexp.MustCompile(`(?m)^\s*import\s+(?:"([^"]+)"|'([^']+)')\s*;|^\s*import\s+[^;]*\s+from\s+"([^"]+)"\s*;|^\s*import\s+[^;]*\s+from\s+'([^']+)'\s*;`)

// linkPos represents a position range within bytecode that is subject to library linking.
type linkPos struct {
	Start  int `json:"start"`
	Length int `json:"length"`
}

// flattenLinkReferences converts Solidity standard JSON linkReferences structure
// into a simple list of start/length positions.
func flattenLinkReferences(m map[string]map[string][]linkPos) []linkPos {
	if m == nil {
		return nil
	}
	out := make([]linkPos, 0, 8)
	for _, inner := range m {
		for _, arr := range inner {
			out = append(out, arr...)
		}
	}
	return out
}

// maskBytesAtPositions returns a copy of data with the specified ranges zeroed.
func maskBytesAtPositions(data []byte, positions []linkPos) []byte {
	if len(positions) == 0 || len(data) == 0 {
		return data
	}
	out := make([]byte, len(data))
	copy(out, data)
	for _, p := range positions {
		if p.Start < 0 || p.Length <= 0 {
			continue
		}
		end := p.Start + p.Length
		if p.Start >= len(out) {
			continue
		}
		if end > len(out) {
			end = len(out)
		}
		for i := p.Start; i < end; i++ {
			out[i] = 0
		}
	}
	return out
}

// combinePositions returns a new slice containing positions from a and b
func combinePositions(a, b []linkPos) []linkPos {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make([]linkPos, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

// firstDiff returns the first index where a and b differ, or -1 if equal up to min length (or lengths equal)
func firstDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}

// compareCreationWithMask compares compiled creation bytecode with the transaction input,
// masking library link reference positions and trimming metadata.
func compareCreationWithMask(tx *types.Transaction, code string, linkRefs []linkPos) (bool, error) {
	bc, err := hexutil.Decode(code)
	if err != nil {
		return false, err
	}
	if len(bc) == 0 {
		return false, nil
	}
	// trim metadata tail in compiled
	bc = cutCodeMetadata(bc)
	// mask compiled ranges (should already be zero, but ensure consistency)
	bc = maskBytesAtPositions(bc, linkRefs)
	if len(tx.InputData) < len(bc) {
		return false, nil
	}
	// take the same prefix length from tx input and mask there as well
	in := tx.InputData[:len(bc)]
	in = maskBytesAtPositions(in, linkRefs)
	return bytes.Equal(bc, in), nil
}

// collectSources resolves and fetches all imported sources reachable from the primary source.
// Keys of the returned map are virtual paths used by solc (must be stable and support relative resolution).
func collectSources(primaryName string, primaryContent string) (map[string]string, error) {
	// sources holds virtual path -> content
	sources := map[string]string{primaryName: primaryContent}

	// visited tracks already processed virtual paths
	visited := map[string]bool{}

	// recursive DFS resolver
	var visit func(curName string) error
	visit = func(curName string) error {
		if visited[curName] {
			return nil
		}
		visited[curName] = true
		content, ok := sources[curName]
		if !ok {
			return fmt.Errorf("source missing: %s", curName)
		}

		// find imports
		matches := importRegexp.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			// submatches may be in groups 1..4 depending on form
			var imp string
			for i := 1; i < len(m); i++ {
				if m[i] != "" {
					imp = m[i]
					break
				}
			}
			if imp == "" {
				continue
			}

			// resolve relative paths against current virtual path
			resolved := imp
			if strings.HasPrefix(imp, ".") {
				base := path.Dir(curName)
				resolved = path.Clean(path.Join(base, imp))
			}

			// if we already have it, just continue
			if _, exists := sources[resolved]; exists {
				if err := visit(resolved); err != nil {
					return err
				}
				continue
			}

			// fetch content for resolved path
			data, err := fetchImport(resolved)
			if err != nil {
				return fmt.Errorf("failed to resolve import %s (from %s): %w", imp, curName, err)
			}
			sources[resolved] = data

			// recurse into the newly fetched file
			if err := visit(resolved); err != nil {
				return err
			}
		}
		return nil
	}

	// start from primary
	if err := visit(primaryName); err != nil {
		return nil, err
	}

	return sources, nil
}

// parseProvidedSources tries to interpret the provided source string as a JSON blob
// that directly contains sources similar to standard-json or Etherscan style.
// Supported forms:
// 1) { "sources": { "path.sol": { "content": "..." }, ... } }
// 2) { "path.sol": { "content": "..." }, ... }
// 3) { "path.sol": "raw content", ... }
// Returns (sources, true) if parsed, otherwise (nil, false).
func parseProvidedSources(source string) (map[string]string, bool) {
	// quick check
	trimmed := strings.TrimSpace(source)
	if !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}
	// shape 1: standard-json-ish with sources
	var withSources struct {
		Sources map[string]struct {
			Content string `json:"content"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(source), &withSources); err == nil && len(withSources.Sources) > 0 {
		out := make(map[string]string, len(withSources.Sources))
		for k, v := range withSources.Sources {
			out[k] = v.Content
		}
		return out, true
	}
	// shape 2: map of file -> {content}
	var mapContent map[string]struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(source), &mapContent); err == nil && len(mapContent) > 0 {
		out := make(map[string]string, len(mapContent))
		for k, v := range mapContent {
			out[k] = v.Content
		}
		return out, true
	}
	// shape 3: map of file -> "raw content"
	var mapRaw map[string]string
	if err := json.Unmarshal([]byte(source), &mapRaw); err == nil && len(mapRaw) > 0 {
		return mapRaw, true
	}
	return nil, false
}

// fetchImport retrieves Solidity source code by virtual key.
// Supported keys:
// - Absolute package paths (e.g., "@openzeppelin/contracts/.../ERC20.sol"): fetched via unpkg.
// - HTTP(S) URLs: fetched directly.
// - GitHub URLs: fetched directly.
// - IPFS hashes: fetched via gateway.
// - NPM packages: fetched via multiple CDNs with fallbacks
// - Local imports: resolved relative to current file
// Other forms are currently unsupported and will return an error.
func fetchImport(key string) (string, error) {
	// direct URL import
	if strings.HasPrefix(key, "http://") || strings.HasPrefix(key, "https://") {
		return httpGetText(key)
	}

	// GitHub URLs (convert to raw content)
	if strings.Contains(key, "github.com") {
		// Convert github.com URLs to raw.githubusercontent.com
		rawURL := strings.Replace(key, "github.com", "raw.githubusercontent.com", 1)
		rawURL = strings.Replace(rawURL, "/blob/", "/", 1)
		return httpGetText(rawURL)
	}

	// IPFS hashes
	if strings.HasPrefix(key, "ipfs://") {
		ipfsHash := strings.TrimPrefix(key, "ipfs://")
		// Try multiple IPFS gateways
		gateways := []string{
			"https://ipfs.io/ipfs/",
			"https://gateway.pinata.cloud/ipfs/",
			"https://cloudflare-ipfs.com/ipfs/",
			"https://dweb.link/ipfs/",
			"https://gateway.ipfs.io/ipfs/",
		}
		for _, gateway := range gateways {
			if content, err := httpGetText(gateway + ipfsHash); err == nil {
				return content, nil
			}
		}
		return "", fmt.Errorf("failed to fetch IPFS content from all gateways: %s", key)
	}

	// NPM-style package import, route via unpkg CDN with fallbacks
	if strings.HasPrefix(key, "@") || strings.Contains(key, "/") {
		// Try multiple CDN endpoints with better fallback strategy
		cdns := []string{
			"https://unpkg.com/",
			"https://cdn.jsdelivr.net/npm/",
			"https://unpkg.com/",
			"https://cdn.skypack.dev/",
			"https://esm.sh/",
			"https://bundle.run/",
		}

		for _, cdn := range cdns {
			url := cdn + strings.TrimPrefix(key, "/")
			if content, err := httpGetText(url); err == nil {
				return content, nil
			}
		}

		// If all CDNs fail, try to extract package name and version
		if strings.HasPrefix(key, "@") {
			parts := strings.Split(key, "/")
			if len(parts) >= 3 {
				// Try to fetch the latest version
				packageName := parts[0] + "/" + parts[1]
				latestURL := "https://registry.npmjs.org/" + packageName + "/latest"
				if content, err := httpGetText(latestURL); err == nil {
					// Parse package.json to get the file path
					var pkg struct {
						Files map[string]string `json:"files"`
					}
					if json.Unmarshal([]byte(content), &pkg) == nil {
						filePath := strings.Join(parts[2:], "/")
						if fileContent, ok := pkg.Files[filePath]; ok {
							return fileContent, nil
						}
					}
				}
			}
		}

		// Special handling for OpenZeppelin contracts
		if strings.Contains(key, "openzeppelin") {
			// Try alternative OpenZeppelin CDN endpoints
			openzeppelinCDNs := []string{
				"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-contracts/master/contracts/",
				"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-contracts/v4.9.3/contracts/",
				"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-contracts/v4.9.0/contracts/",
				"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-contracts/v5.0.0/contracts/",
			}

			for _, cdn := range openzeppelinCDNs {
				// Extract the path after "contracts/"
				if idx := strings.Index(key, "contracts/"); idx != -1 {
					path := key[idx+10:] // "contracts/" is 10 characters
					url := cdn + path
					if content, err := httpGetText(url); err == nil {
						return content, nil
					}
				}
			}
		}

		// Special handling for ALL OpenZeppelin packages
		if strings.Contains(key, "openzeppelin") {
			// Extract package name and version from the import path
			// Examples:
			// @openzeppelin/contracts@4.9.3/interfaces/draft-IERC1822.sol
			// @openzeppelin/upgrades@4.9.3/proxy/utils/Initializable.sol
			// @openzeppelin/defender@1.0.0/contracts/autotasks/.../Autotask.sol

			// Parse the import path to extract package info
			parts := strings.Split(key, "/")
			if len(parts) >= 2 {
				packagePart := parts[0] // @openzeppelin/contracts@4.9.3

				// Extract package name and version
				var packageName, version string
				if strings.Contains(packagePart, "@") {
					// Has version: @openzeppelin/contracts@4.9.3
					atIndex := strings.LastIndex(packagePart, "@")
					packageName = packagePart[:atIndex]
					version = packagePart[atIndex+1:]
				} else {
					// No version: @openzeppelin/contracts
					packageName = packagePart
					version = "latest"
				}

				// Remove @ prefix from package name
				packageName = strings.TrimPrefix(packageName, "@")

				// Define OpenZeppelin package mappings
				openzeppelinPackages := map[string]map[string][]string{
					"openzeppelin/contracts": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-contracts/master/contracts/"},
						"v4.9.3": {"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-contracts/v4.9.3/contracts/"},
						"v4.9.0": {"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-contracts/v4.9.0/contracts/"},
						"v5.0.0": {"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-contracts/v5.0.0/contracts/"},
						"v4.8.0": {"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-contracts/v4.8.0/contracts/"},
						"v4.7.0": {"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-contracts/v4.7.0/contracts/"},
					},
					"openzeppelin/upgrades": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-upgrades/master/contracts/"},
						"v4.9.3": {"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-upgrades/v4.9.3/contracts/"},
						"v4.9.0": {"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-upgrades/v4.9.0/contracts/"},
						"v5.0.0": {"https://raw.githubusercontent.com/OpenZeppelin/openzeppelin-upgrades/v5.0.0/contracts/"},
					},
					"openzeppelin/defender": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/defender/master/contracts/"},
						"v1.0.0": {"https://raw.githubusercontent.com/OpenZeppelin/defender/v1.0.0/contracts/"},
					},
					"openzeppelin/hardhat": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/hardhat-upgrades/master/contracts/"},
					},
					"openzeppelin/truffle": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/truffle-upgrades/master/contracts/"},
					},
					"openzeppelin/test-helpers": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/test-helpers/master/src/"},
						"v0.5.0": {"https://raw.githubusercontent.com/OpenZeppelin/test-helpers/v0.5.0/src/"},
					},
					"openzeppelin/merkle-tree": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-tree/master/src/"},
					},
					"openzeppelin/merkle-utils": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-utils/master/src/"},
					},
					"openzeppelin/merkle-proof": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-proof/master/src/"},
					},
					"openzeppelin/merkle-verifier": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-verifier/master/src/"},
					},
					"openzeppelin/merkle-validator": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-validator/master/src/"},
					},
					"openzeppelin/merkle-checker": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-checker/master/src/"},
					},
					"openzeppelin/merkle-verification": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-verification/master/src/"},
					},
					"openzeppelin/merkle-validation": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-validation/master/src/"},
					},
					"openzeppelin/merkle-checking": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-checking/master/src/"},
					},
					"openzeppelin/merkle-verifying": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-verifying/master/src/"},
					},
					"openzeppelin/merkle-validating": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-validating/master/src/"},
					},
					"openzeppelin/merkle-checking-utils": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-checking-utils/master/src/"},
					},
					"openzeppelin/merkle-verification-utils": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-verification-utils/master/src/"},
					},
					"openzeppelin/merkle-validation-utils": {
						"latest": {"https://raw.githubusercontent.com/OpenZeppelin/merkle-validation-utils/master/src/"},
					},
				}

				// Try to find the package and version
				if packageURLs, packageExists := openzeppelinPackages[packageName]; packageExists {
					// Try specific version first
					if urls, versionExists := packageURLs[version]; versionExists {
						for _, baseURL := range urls {
							// Construct the file path
							filePath := strings.Join(parts[1:], "/")
							url := baseURL + filePath
							if content, err := httpGetText(url); err == nil {
								return content, nil
							}
						}
					}

					// Fallback to latest version
					if urls, latestExists := packageURLs["latest"]; latestExists {
						for _, baseURL := range urls {
							filePath := strings.Join(parts[1:], "/")
							url := baseURL + filePath
							if content, err := httpGetText(url); err == nil {
								return content, nil
							}
						}
					}
				}

				// If not found in specific mappings, try generic OpenZeppelin patterns
				genericPatterns := []string{
					fmt.Sprintf("https://raw.githubusercontent.com/OpenZeppelin/%s/master/contracts/", strings.TrimPrefix(packageName, "openzeppelin/")),
					fmt.Sprintf("https://raw.githubusercontent.com/OpenZeppelin/%s/master/src/", strings.TrimPrefix(packageName, "openzeppelin/")),
					fmt.Sprintf("https://raw.githubusercontent.com/OpenZeppelin/%s/%s/contracts/", strings.TrimPrefix(packageName, "openzeppelin/"), version),
					fmt.Sprintf("https://raw.githubusercontent.com/OpenZeppelin/%s/%s/src/", strings.TrimPrefix(packageName, "openzeppelin/"), version),
				}

				for _, pattern := range genericPatterns {
					filePath := strings.Join(parts[1:], "/")
					url := pattern + filePath
					if content, err := httpGetText(url); err == nil {
						return content, nil
					}
				}
			}
		}

		// Special handling for other popular packages
		popularPackages := map[string][]string{
			"hardhat": {
				"https://raw.githubusercontent.com/NomicFoundation/hardhat/master/packages/hardhat-core/src/internal/core/config/",
				"https://raw.githubusercontent.com/NomicFoundation/hardhat/master/packages/hardhat-core/src/internal/",
			},
			"forge-std": {
				"https://raw.githubusercontent.com/foundry-rs/forge-std/master/src/",
				"https://raw.githubusercontent.com/foundry-rs/forge-std/v1.7.1/src/",
			},
			"solmate": {
				"https://raw.githubusercontent.com/transmissions11/solmate/main/src/",
				"https://raw.githubusercontent.com/transmissions11/solmate/v7/src/",
			},
			"ds-test": {
				"https://raw.githubusercontent.com/dapphub/ds-test/master/src/",
			},
			"chainlink": {
				"https://raw.githubusercontent.com/smartcontractkit/chainlink/master/contracts/",
				"https://raw.githubusercontent.com/smartcontractkit/chainlink/v2.5.0/contracts/",
			},
			// Source URLs for resolving Solidity IMPORTS during contract verification.
			// Unrelated to the Uniswap DeFi module that was removed from this explorer:
			// a user can verify a contract importing Uniswap interfaces whether or not
			// this chain runs Uniswap.
			"uniswap": {
				"https://raw.githubusercontent.com/Uniswap/v3-core/main/contracts/",
				"https://raw.githubusercontent.com/Uniswap/v2-core/master/contracts/",
			},
			"aave": {
				"https://raw.githubusercontent.com/aave/aave-v3-core/main/contracts/",
				"https://raw.githubusercontent.com/aave/aave-v2-core/master/contracts/",
			},
			"compound": {
				"https://raw.githubusercontent.com/compound-finance/compound-protocol/master/contracts/",
			},
			"dappsys": {
				"https://raw.githubusercontent.com/dapphub/dappsys-monolithic/master/src/",
			},
		}

		// Check if this is a known popular package
		for pkgName, urls := range popularPackages {
			if strings.Contains(key, pkgName) {
				for _, baseURL := range urls {
					// Try to construct the URL by removing the package name prefix
					if strings.HasPrefix(key, pkgName) {
						path := strings.TrimPrefix(key, pkgName)
						path = strings.TrimPrefix(path, "/")
						url := baseURL + path
						if content, err := httpGetText(url); err == nil {
							return content, nil
						}
					}
				}
			}
		}

		// Try GitHub-style resolution for unknown packages
		// This handles cases like "package/contracts/Contract.sol"
		if strings.Contains(key, "/") && !strings.HasPrefix(key, "@") {
			parts := strings.Split(key, "/")
			if len(parts) >= 2 {
				// Try common GitHub patterns
				githubPatterns := []string{
					fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/contracts/", parts[0], parts[1]),
					fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/contracts/", parts[0], parts[1]),
					fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/src/", parts[0], parts[1]),
					fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/src/", parts[0], parts[1]),
				}

				for _, pattern := range githubPatterns {
					path := strings.Join(parts[2:], "/")
					url := pattern + path
					if content, err := httpGetText(url); err == nil {
						return content, nil
					}
				}
			}
		}

		return "", fmt.Errorf("failed to fetch package from all CDNs: %s", key)
	}

	return "", fmt.Errorf("unsupported import path: %s", key)
}

// httpGetText performs a simple GET request and returns the body as string if status is 200.
func httpGetText(url string) (string, error) {
	// use a short-lived client; resolver depth is small
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	// set basic headers to improve CDN compatibility
	req.Header.Set("User-Agent", "ncogearthchain-api-graphql/solidity-import-resolver")

	// default client with timeout via context from caller is not available here; rely on transport defaults
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, url, string(body))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// compileSolidityStandardJSON compiles a single-source Solidity input using solc --standard-json
// and respects optimizer settings. Optional evmVersion and viaIR can be provided to match build.
func compileSolidityStandardJSON(solcPath string, source string, optimized bool, runs int32, evmVersion string, viaIR bool) (map[string]compiledArtifact, error) {
	// resolve sources: either provided as a JSON bundle, or collect from a primary file and imports
	var sourcesCollected map[string]string
	if srcs, ok := parseProvidedSources(source); ok {
		sourcesCollected = srcs
	} else {
		var err error
		sourcesCollected, err = collectSources("input.sol", source)
		if err != nil {
			return nil, err
		}
	}

	// build standard-json input
	type sourceContent struct {
		Content string `json:"content"`
	}
	type stdIn struct {
		Language string                   `json:"language"`
		Sources  map[string]sourceContent `json:"sources"`
		Settings struct {
			Optimizer struct {
				Enabled bool  `json:"enabled"`
				Runs    int32 `json:"runs"`
			} `json:"optimizer"`
			OutputSelection map[string]map[string][]string `json:"outputSelection"`
			Remappings      []string                       `json:"remappings,omitempty"`
		} `json:"settings"`
	}
	in := stdIn{Language: "Solidity", Sources: map[string]sourceContent{}}
	for name, content := range sourcesCollected {
		in.Sources[name] = sourceContent{Content: content}
	}
	in.Settings.Optimizer.Enabled = optimized
	if runs < 0 {
		runs = 0
	}
	in.Settings.Optimizer.Runs = runs
	in.Settings.OutputSelection = map[string]map[string][]string{
		"*": {
			"*": {"abi", "metadata", "evm.bytecode.object", "evm.bytecode.linkReferences", "evm.deployedBytecode.object", "evm.deployedBytecode.linkReferences", "evm.deployedBytecode.immutableReferences"},
		},
	}
	// add optional settings
	type evmAndIR struct{}
	_ = evmAndIR{}
	// inject evmVersion and viaIR by re-marshalling with extra fields
	// since stdIn.Settings is an anonymous struct, rebuild via a map envelope
	var settings map[string]interface{}
	b, _ := json.Marshal(in.Settings)
	_ = json.Unmarshal(b, &settings)
	if evmVersion != "" {
		settings["evmVersion"] = evmVersion
	}
	if viaIR {
		settings["viaIR"] = true
	}
	// rebuild full std json input
	raw := map[string]interface{}{
		"language": in.Language,
		"sources":  map[string]map[string]string{},
		"settings": settings,
	}
	ss := raw["sources"].(map[string]map[string]string)
	for k, v := range in.Sources {
		ss[k] = map[string]string{"content": v.Content}
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	// run solc --standard-json
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, solcPath, "--standard-json")
	cmd.Stdin = bytes.NewReader(payload)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("solc standard-json failed: %w", err)
	}

	// parse output
	var stdOut struct {
		Contracts map[string]map[string]struct {
			Abi json.RawMessage `json:"abi"`
			Evm struct {
				Bytecode struct {
					Object         string                          `json:"object"`
					LinkReferences map[string]map[string][]linkPos `json:"linkReferences"`
				} `json:"bytecode"`
				DeployedBytecode struct {
					Object              string                          `json:"object"`
					LinkReferences      map[string]map[string][]linkPos `json:"linkReferences"`
					ImmutableReferences map[string][]linkPos            `json:"immutableReferences"`
				} `json:"deployedBytecode"`
			} `json:"evm"`
			Metadata string `json:"metadata"`
		} `json:"contracts"`
		Version string `json:"version"`
		Errors  []struct {
			Type     string `json:"type"`
			Message  string `json:"message"`
			Severity string `json:"severity"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(out, &stdOut); err != nil {
		return nil, fmt.Errorf("failed to parse solc output: %w", err)
	}
	// if any error-severity entries exist, surface first
	for _, e := range stdOut.Errors {
		if e.Severity == "error" {
			return nil, fmt.Errorf("solc error: %s", e.Message)
		}
	}

	artifacts := make(map[string]compiledArtifact)
	for _, byName := range stdOut.Contracts {
		for name, c := range byName {
			art := compiledArtifact{
				Name:             name,
				Code:             hexWithPrefix(c.Evm.Bytecode.Object),
				RuntimeCode:      hexWithPrefix(c.Evm.DeployedBytecode.Object),
				Abi:              c.Abi,
				Metadata:         c.Metadata,
				CompilerVersion:  stdOut.Version,
				CreationLinkRefs: flattenLinkReferences(c.Evm.Bytecode.LinkReferences),
				RuntimeLinkRefs:  flattenLinkReferences(c.Evm.DeployedBytecode.LinkReferences),
				RuntimeImmutableRefs: func() []linkPos {
					// c.Evm.DeployedBytecode.ImmutableReferences is map[string][]linkPos
					if c.Evm.DeployedBytecode.ImmutableReferences == nil {
						return nil
					}
					out := make([]linkPos, 0, 8)
					for _, arr := range c.Evm.DeployedBytecode.ImmutableReferences {
						out = append(out, arr...)
					}
					return out
				}(),
			}
			artifacts[name] = art
		}
	}
	return artifacts, nil
}

func hexWithPrefix(h string) string {
	if len(h) == 0 {
		return "0x"
	}
	if strings.HasPrefix(h, "0x") || strings.HasPrefix(h, "0X") {
		return h
	}
	return "0x" + h
}

// tryExtractEIP1167Target tries to extract the implementation address from an EIP-1167 minimal proxy runtime.
// Returns zero address if not a recognized minimal proxy.
func tryExtractEIP1167Target(runtime []byte) common.Address {
	// common EIP-1167 runtime: 0x363d3d373d3d3d363d73 <20-byte impl> 5af43d82803e903d91602b57fd5bf3
	// look for marker 363d3d373d3d3d363d73 (10 bytes) then 20 bytes address then 5af43d82803e903d91602b57fd5bf3
	markerPrefix := []byte{0x36, 0x3d, 0x3d, 0x37, 0x3d, 0x3d, 0x3d, 0x36, 0x3d, 0x73}
	markerSuffix := []byte{0x5a, 0xf4, 0x3d, 0x82, 0x80, 0x3e, 0x90, 0x3d, 0x91, 0x60, 0x2b, 0x57, 0xfd, 0x5b, 0xf3}
	// naive scan
	for i := 0; i+len(markerPrefix)+20+len(markerSuffix) <= len(runtime); i++ {
		if bytes.Equal(runtime[i:i+len(markerPrefix)], markerPrefix) {
			addrStart := i + len(markerPrefix)
			addrEnd := addrStart + 20
			if bytes.Equal(runtime[addrEnd:addrEnd+len(markerSuffix)], markerSuffix) {
				return common.BytesToAddress(runtime[addrStart:addrEnd])
			}
		}
	}
	return common.Address{}
}

// tryExtractCustomProxyTarget tries to extract implementation address from custom proxy patterns
func tryExtractCustomProxyTarget(runtime []byte) common.Address {
	// Try to find common proxy patterns
	patterns := []struct {
		name   string
		prefix []byte
		suffix []byte
	}{
		{
			name:   "EIP-1167",
			prefix: []byte{0x36, 0x3d, 0x3d, 0x37, 0x3d, 0x3d, 0x3d, 0x36, 0x3d, 0x73},
			suffix: []byte{0x5a, 0xf4, 0x3d, 0x82, 0x80, 0x3e, 0x90, 0x3d, 0x91, 0x60, 0x2b, 0x57, 0xfd, 0x5b, 0xf3},
		},
		{
			name:   "EIP-1167 Alternative",
			prefix: []byte{0x3d, 0x3d, 0x93, 0x3d, 0x3d, 0x36, 0x3d, 0x37, 0x36, 0x5a},
			suffix: []byte{0x5a, 0xf4, 0x3d, 0x82, 0x80, 0x3e, 0x90, 0x3d, 0x91, 0x60, 0x2b, 0x57, 0xfd, 0x5b, 0xf3},
		},
		{
			name:   "Minimal Proxy",
			prefix: []byte{0x60, 0x2d, 0x60, 0x2d, 0x60, 0x2d, 0x60, 0x2d, 0x60, 0x2d},
			suffix: []byte{0x5a, 0xf4, 0x3d, 0x82, 0x80, 0x3e, 0x90, 0x3d, 0x91, 0x60, 0x2b, 0x57, 0xfd, 0x5b, 0xf3},
		},
		{
			name:   "EIP-1167 with PUSH20",
			prefix: []byte{0x60, 0x2b, 0x60, 0x2d, 0x60, 0x2d, 0x60, 0x2d, 0x60, 0x2d},
			suffix: []byte{0x5a, 0xf4, 0x3d, 0x82, 0x80, 0x3e, 0x90, 0x3d, 0x91, 0x60, 0x2b, 0x57, 0xfd, 0x5b, 0xf3},
		},
		{
			name:   "Custom Proxy Pattern 1",
			prefix: []byte{0x60, 0x2d, 0x60, 0x2d, 0x60, 0x2d, 0x60, 0x2d, 0x60, 0x2d, 0x60, 0x2d},
			suffix: []byte{0x5a, 0xf4, 0x3d, 0x82, 0x80, 0x3e, 0x90, 0x3d, 0x91, 0x60, 0x2b, 0x57, 0xfd, 0x5b, 0xf3},
		},
	}

	for _, pattern := range patterns {
		for i := 0; i+len(pattern.prefix)+20+len(pattern.suffix) <= len(runtime); i++ {
			if bytes.Equal(runtime[i:i+len(pattern.prefix)], pattern.prefix) {
				addrStart := i + len(pattern.prefix)
				addrEnd := addrStart + 20
				if addrEnd+len(pattern.suffix) <= len(runtime) && bytes.Equal(runtime[addrEnd:addrEnd+len(pattern.suffix)], pattern.suffix) {
					return common.BytesToAddress(runtime[addrStart:addrEnd])
				}
			}
		}
	}
	return common.Address{}
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

	// try to compile the source code provided with explicit optimizer settings
	compiled, err := compileSolidityStandardJSON(compilerPath, sc.SourceCode, sc.IsOptimized, sc.OptimizeRuns, sc.EvmVersion, sc.ViaIR)
	p.log.Debugf("compiled output: %+v", compiled)
	if err != nil {
		p.log.Errorf("solidity code compilation failed with compiler %s: %s", compilerPath, err.Error())

		// Log additional debugging information for proxy-related issues
		if strings.Contains(sc.SourceCode, "@openzeppelin") || strings.Contains(sc.SourceCode, "proxy") {
			p.log.Errorf("proxy-related contract compilation failed. Source code length: %d, contains OpenZeppelin: %v",
				len(sc.SourceCode), strings.Contains(sc.SourceCode, "@openzeppelin"))

			// Check for import statements that might be failing
			importMatches := importRegexp.FindAllStringSubmatch(sc.SourceCode, -1)
			if len(importMatches) > 0 {
				p.log.Errorf("found %d import statements in source code", len(importMatches))
				for i, match := range importMatches {
					for j, submatch := range match {
						if j > 0 && submatch != "" {
							p.log.Errorf("import %d: %s", i+1, submatch)

							// Try to categorize the import type
							if strings.HasPrefix(submatch, "@") {
								p.log.Errorf("  - NPM package import detected")
							} else if strings.Contains(submatch, "github.com") {
								p.log.Errorf("  - GitHub URL import detected")
							} else if strings.HasPrefix(submatch, "ipfs://") {
								p.log.Errorf("  - IPFS import detected")
							} else if strings.HasPrefix(submatch, "http") {
								p.log.Errorf("  - HTTP/HTTPS import detected")
							} else if strings.Contains(submatch, "/") {
								p.log.Errorf("  - Local/relative import detected")
							} else {
								p.log.Errorf("  - Standard library or local import detected")
							}
						}
					}
				}
			}

			// Log compiler settings being used
			p.log.Errorf("compiler settings: optimized=%v, runs=%d, evmVersion=%s, viaIR=%v",
				sc.IsOptimized, sc.OptimizeRuns, sc.EvmVersion, sc.ViaIR)

			// Check for specific proxy patterns in source code
			if strings.Contains(sc.SourceCode, "UUPSUpgradeable") {
				p.log.Errorf("detected UUPS proxy pattern in source code")
			}
			if strings.Contains(sc.SourceCode, "TransparentUpgradeableProxy") {
				p.log.Errorf("detected Transparent proxy pattern in source code")
			}
			if strings.Contains(sc.SourceCode, "BeaconProxy") {
				p.log.Errorf("detected Beacon proxy pattern in source code")
			}
			if strings.Contains(sc.SourceCode, "_authorizeUpgrade") {
				p.log.Errorf("detected UUPS authorization function in source code")
			}

			// Enhanced OpenZeppelin package detection
			openzeppelinImports := []string{}
			for _, match := range importMatches {
				for _, submatch := range match {
					if strings.Contains(submatch, "openzeppelin") {
						openzeppelinImports = append(openzeppelinImports, submatch)
					}
				}
			}
			if len(openzeppelinImports) > 0 {
				p.log.Errorf("found %d OpenZeppelin imports:", len(openzeppelinImports))
				for i, imp := range openzeppelinImports {
					p.log.Errorf("  OpenZeppelin import %d: %s", i+1, imp)

					// Parse OpenZeppelin package info
					if strings.Contains(imp, "@") {
						parts := strings.Split(imp, "/")
						if len(parts) >= 1 {
							packagePart := parts[0]
							if strings.Contains(packagePart, "@") {
								atIndex := strings.LastIndex(packagePart, "@")
								packageName := packagePart[:atIndex]
								version := packagePart[atIndex+1:]
								p.log.Errorf("    Package: %s, Version: %s", packageName, version)
							} else {
								p.log.Errorf("    Package: %s, Version: latest", packagePart)
							}
						}
					}
				}
			}
		} else {
			// General import resolution logging for non-proxy contracts
			importMatches := importRegexp.FindAllStringSubmatch(sc.SourceCode, -1)
			if len(importMatches) > 0 {
				p.log.Errorf("found %d import statements in source code", len(importMatches))
				for i, match := range importMatches {
					for j, submatch := range match {
						if j > 0 && submatch != "" {
							p.log.Errorf("import %d: %s", i+1, submatch)

							// Try to categorize the import type
							if strings.HasPrefix(submatch, "@") {
								p.log.Errorf("  - NPM package import detected")
							} else if strings.Contains(submatch, "github.com") {
								p.log.Errorf("  - GitHub URL import detected")
							} else if strings.HasPrefix(submatch, "ipfs://") {
								p.log.Errorf("  - IPFS import detected")
							} else if strings.HasPrefix(submatch, "http") {
								p.log.Errorf("  - HTTP/HTTPS import detected")
							} else if strings.Contains(submatch, "/") {
								p.log.Errorf("  - Local/relative import detected")
							} else {
								p.log.Errorf("  - Standard library or local import detected")
							}
						}
					}
				}
			}

			// Log compiler settings being used
			p.log.Errorf("compiler settings: optimized=%v, runs=%d, evmVersion=%s, viaIR=%v",
				sc.IsOptimized, sc.OptimizeRuns, sc.EvmVersion, sc.ViaIR)

			// Try compilation with different EVM versions if not set
			if sc.EvmVersion == "" {
				p.log.Errorf("EVM version not set, trying common EVM versions")
				commonEvmVersions := []string{"paris", "shanghai", "london", "berlin", "istanbul"}
				for _, evm := range commonEvmVersions {
					p.log.Errorf("trying EVM version: %s", evm)
					if altCompiled, altErr := compileSolidityStandardJSON(compilerPath, sc.SourceCode, sc.IsOptimized, sc.OptimizeRuns, evm, sc.ViaIR); altErr == nil {
						p.log.Errorf("compilation succeeded with EVM version: %s", evm)
						compiled = altCompiled
						sc.EvmVersion = evm
						err = nil
						break
					} else {
						p.log.Errorf("compilation failed with EVM version %s: %s", evm, altErr.Error())
					}
				}
			}
		}

		if err != nil {
			return err
		}
	}

	// Check if this is a UUPS proxy contract early in the process
	if isUUPSProxy(sc.Address, p.rpc) {
		p.log.Debugf("detected UUPS proxy contract: %s", sc.Address.Hex())

		// Try the dedicated UUPS verification function first
		if err := verifyUUPSProxyContract(sc, compiled, p.rpc, p.log); err == nil {
			// Successfully verified UUPS proxy - update contract details
			for name, detail := range compiled {
				trimmedName := strings.TrimPrefix(name, "<stdin>:")
				if len(sc.Name) > 0 && sc.Name != trimmedName {
					continue
				}

				// Update contract details
				if len(sc.Name) == 0 {
					sc.Name = trimmedName
				}
				sc.Compiler = fmt.Sprintf("Solidity %s", detail.CompilerVersion)
				if len(detail.Abi) > 0 {
					sc.Abi = string(detail.Abi)
				}
				if len(detail.Metadata) > 0 {
					sc.Metadata = detail.Metadata
				}
				// capture bytecodes and link/immutable references for UI
				sc.CreationBytecode = detail.Code
				sc.RuntimeBytecode = detail.RuntimeCode
				sc.Version = detail.CompilerVersion
				sc.CreationLinkReferences = make([]types.LinkReferenceRange, len(detail.CreationLinkRefs))
				for i, r := range detail.CreationLinkRefs {
					sc.CreationLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
				}
				sc.RuntimeLinkReferences = make([]types.LinkReferenceRange, len(detail.RuntimeLinkRefs))
				for i, r := range detail.RuntimeLinkRefs {
					sc.RuntimeLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
				}
				sc.RuntimeImmutableReferences = make([]types.LinkReferenceRange, len(detail.RuntimeImmutableRefs))
				for i, r := range detail.RuntimeImmutableRefs {
					sc.RuntimeImmutableReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
				}
				now := hexutil.Uint64(uint64(time.Now().Unix()))
				sc.Validated = &now
				if err := p.pg.UpdateContractValidation(storeCtx(), sc); err != nil {
					p.log.Errorf("contract validation (UUPS impl) failed due to db error; %s", err.Error())
					return err
				}
				p.log.Debugf("contract %s [%s] validated by UUPS implementation runtime with compiler %s", sc.Address.String(), name, compilerPath)
				p.cache.EvictContract(&sc.Address)
				return nil
			}
		} else {
			p.log.Debugf("UUPS verification failed, trying fallback methods: %s", err.Error())
		}

		// Get implementation address
		implAddr, err := getUUPSImplementation(sc.Address, p.rpc)
		if err != nil {
			p.log.Errorf("failed to get UUPS implementation: %s", err.Error())
		} else {
			p.log.Debugf("UUPS implementation address: %s", implAddr.Hex())

			// Try to verify against implementation contract first
			if implCode, err2 := p.rpc.ContractCode(&implAddr); err2 == nil && len(implCode) > 0 {
				p.log.Debugf("implementation code length: %d", len(implCode))

				// Try to match compiled source code with implementation bytecode
				for name, detail := range compiled {
					trimmedName := strings.TrimPrefix(name, "<stdin>:")
					if len(sc.Name) > 0 && sc.Name != trimmedName {
						continue
					}

					runtimeHex := detail.RuntimeCode
					if len(runtimeHex) <= 2 {
						continue
					}

					compiledRuntime, err3 := hexutil.Decode(runtimeHex)
					if err3 != nil || len(compiledRuntime) == 0 {
						continue
					}

					// Remove metadata and mask link references
					compiledRuntime = cutCodeMetadata(compiledRuntime)
					compiledRuntime = maskBytesAtPositions(compiledRuntime, combinePositions(detail.RuntimeLinkRefs, detail.RuntimeImmutableRefs))

					if len(implCode) >= len(compiledRuntime) {
						maskedOnChain := maskBytesAtPositions(implCode[:len(compiledRuntime)], detail.RuntimeLinkRefs)
						// debug: print snippet of compiled vs implementation masked code
						head := 64
						if len(compiledRuntime) < head {
							head = len(compiledRuntime)
						}
						if len(maskedOnChain) < head {
							head = len(maskedOnChain)
						}
						p.log.Infof("runtime compare (impl): compiled len=%d, impl len=%d", len(compiledRuntime), len(implCode))
						if head > 0 {
							p.log.Infof("compiled head=%s", hexutil.Encode(compiledRuntime[:head]))
							p.log.Infof("impl     head=%s", hexutil.Encode(maskedOnChain[:head]))
						}
						if bytes.Equal(compiledRuntime, maskedOnChain) {
							// Successfully matched UUPS implementation!
							if len(sc.Name) == 0 {
								sc.Name = trimmedName
							}
							sc.Compiler = fmt.Sprintf("Solidity %s", detail.CompilerVersion)
							if len(detail.Abi) > 0 {
								sc.Abi = string(detail.Abi)
							}
							if len(detail.Metadata) > 0 {
								sc.Metadata = detail.Metadata
							}
							// capture bytecodes and link/immutable references for UI
							sc.CreationBytecode = detail.Code
							sc.RuntimeBytecode = detail.RuntimeCode
							sc.Version = detail.CompilerVersion
							sc.CreationLinkReferences = make([]types.LinkReferenceRange, len(detail.CreationLinkRefs))
							for i, r := range detail.CreationLinkRefs {
								sc.CreationLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
							}
							sc.RuntimeLinkReferences = make([]types.LinkReferenceRange, len(detail.RuntimeLinkRefs))
							for i, r := range detail.RuntimeLinkRefs {
								sc.RuntimeLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
							}
							sc.RuntimeImmutableReferences = make([]types.LinkReferenceRange, len(detail.RuntimeImmutableRefs))
							for i, r := range detail.RuntimeImmutableRefs {
								sc.RuntimeImmutableReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
							}
							now := hexutil.Uint64(uint64(time.Now().Unix()))
							sc.Validated = &now
							if err := p.pg.UpdateContractValidation(storeCtx(), sc); err != nil {
								p.log.Errorf("contract validation (UUPS impl) failed due to db error; %s", err.Error())
								return err
							}
							p.log.Debugf("contract %s [%s] validated by UUPS implementation runtime at %s with compiler %s", sc.Address.String(), name, implAddr.Hex(), compilerPath)
							p.cache.EvictContract(&sc.Address)
							return nil
						}
					}
				}
			}
		}
	}

	// loop over contracts and try to validate one of them
	for name, detail := range compiled {
		// if a specific contract name is provided, consider only that artifact
		trimmedName := strings.TrimPrefix(name, "<stdin>:")
		if len(sc.Name) > 0 && sc.Name != trimmedName {
			continue
		}
		// check if the compiled byte code match with the deployed contract
		// prefer masked comparison to handle potential library link references
		matchMasked, err := compareCreationWithMask(tx, detail.Code, detail.CreationLinkRefs)
		if err != nil {
			p.log.Errorf("contract byte code comparison failed")
			return err
		}

		// we have the winner
		if matchMasked {
			// set the contract name if not done already
			if len(sc.Name) == 0 {
				sc.Name = strings.TrimPrefix(name, "<stdin>:")
			}

			// update the contract data
			// update details
			sc.Compiler = fmt.Sprintf("Solidity %s", detail.CompilerVersion)
			if len(detail.Abi) > 0 {
				sc.Abi = string(detail.Abi)
			}
			if len(detail.Metadata) > 0 {
				sc.Metadata = detail.Metadata
			}

			// capture bytecodes and link/immutable references for UI
			sc.CreationBytecode = detail.Code
			sc.RuntimeBytecode = detail.RuntimeCode
			sc.Version = detail.CompilerVersion
			sc.CreationLinkReferences = make([]types.LinkReferenceRange, len(detail.CreationLinkRefs))
			for i, r := range detail.CreationLinkRefs {
				sc.CreationLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
			}
			sc.RuntimeLinkReferences = make([]types.LinkReferenceRange, len(detail.RuntimeLinkRefs))
			for i, r := range detail.RuntimeLinkRefs {
				sc.RuntimeLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
			}
			sc.RuntimeImmutableReferences = make([]types.LinkReferenceRange, len(detail.RuntimeImmutableRefs))
			for i, r := range detail.RuntimeImmutableRefs {
				sc.RuntimeImmutableReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
			}

			// set validated time stamp (now)
			now := hexutil.Uint64(uint64(time.Now().Unix()))
			sc.Validated = &now

			// write update to the database
			if err := p.pg.UpdateContractValidation(storeCtx(), sc); err != nil {
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
	// Fetch on-chain runtime code of the provided address (may be proxy)
	onChainCode, err := p.rpc.ContractCode(&sc.Address)
	if err == nil && len(onChainCode) > 0 {
		// try to match runtime code at the proxy (for non-proxy or beacon-proxy with impl equal)
		for name, detail := range compiled {
			// restrict to requested contract name if provided
			trimmedName := strings.TrimPrefix(name, "<stdin>:")
			if len(sc.Name) > 0 && sc.Name != trimmedName {
				continue
			}
			// use runtime bytecode provided by the compiler output
			runtimeHex := detail.RuntimeCode
			// skip if runtime is empty or just "0x"
			if len(runtimeHex) <= 2 {
				// no runtime code available for this artifact
				continue
			}

			compiledRuntime, err := hexutil.Decode(runtimeHex)
			if err != nil {
				continue
			}
			// skip artifacts with empty runtime code
			if len(compiledRuntime) == 0 {
				continue
			}
			// strip metadata tail from compiled runtime as well and mask link refs
			compiledRuntime = cutCodeMetadata(compiledRuntime)
			compiledRuntime = maskBytesAtPositions(compiledRuntime, combinePositions(detail.RuntimeLinkRefs, detail.RuntimeImmutableRefs))

			// compare equality with on-chain code prefix-equality or exact? Use exact length match
			if len(onChainCode) >= len(compiledRuntime) {
				// mask the same positions on on-chain code slice
				// mask with both link and immutable refs
				maskedOnChain := maskBytesAtPositions(onChainCode[:len(compiledRuntime)], combinePositions(detail.RuntimeLinkRefs, detail.RuntimeImmutableRefs))
				// debug: print snippet of compiled vs on-chain masked code
				head := 64
				if len(compiledRuntime) < head {
					head = len(compiledRuntime)
				}
				if len(maskedOnChain) < head {
					head = len(maskedOnChain)
				}
				p.log.Infof("runtime compare (proxy): compiled len=%d, onchain len=%d", len(compiledRuntime), len(onChainCode))
				if head > 0 {
					p.log.Infof("compiled head=%s", hexutil.Encode(compiledRuntime[:head]))
					p.log.Infof("onchain  head=%s", hexutil.Encode(maskedOnChain[:head]))
				}
				if bytes.Equal(compiledRuntime, maskedOnChain) {
					// matched by runtime
					if len(sc.Name) == 0 {
						sc.Name = trimmedName
					}
					// update details
					sc.Compiler = fmt.Sprintf("Solidity %s", detail.CompilerVersion)
					if len(detail.Abi) > 0 {
						sc.Abi = string(detail.Abi)
					}
					if len(detail.Metadata) > 0 {
						sc.Metadata = detail.Metadata
					}
					// capture bytecodes and link/immutable references for UI
					sc.CreationBytecode = detail.Code
					sc.RuntimeBytecode = detail.RuntimeCode
					sc.Version = detail.CompilerVersion
					sc.CreationLinkReferences = make([]types.LinkReferenceRange, len(detail.CreationLinkRefs))
					for i, r := range detail.CreationLinkRefs {
						sc.CreationLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
					}
					sc.RuntimeLinkReferences = make([]types.LinkReferenceRange, len(detail.RuntimeLinkRefs))
					for i, r := range detail.RuntimeLinkRefs {
						sc.RuntimeLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
					}
					sc.RuntimeImmutableReferences = make([]types.LinkReferenceRange, len(detail.RuntimeImmutableRefs))
					for i, r := range detail.RuntimeImmutableRefs {
						sc.RuntimeImmutableReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
					}

					// set validated time stamp (now)
					now := hexutil.Uint64(uint64(time.Now().Unix()))
					sc.Validated = &now

					if err := p.pg.UpdateContractValidation(storeCtx(), sc); err != nil {
						p.log.Errorf("contract validation (runtime) failed due to db error; %s", err.Error())
						return err
					}
					p.log.Debugf("contract %s [%s] validated by runtime bytecode with compiler %s", sc.Address.String(), name, compilerPath)
					p.cache.EvictContract(&sc.Address)
					return nil
				}
			}
		}

		// If runtime didn't match at the proxy address, attempt various proxy detection methods
		// First try EIP-1167 minimal proxy detection
		if implFromClone := tryExtractEIP1167Target(onChainCode); implFromClone != (common.Address{}) {
			p.log.Debugf("detected EIP-1167 clone; impl=%s", implFromClone.Hex())
			if implCode, err2 := p.rpc.ContractCode(&implFromClone); err2 == nil && len(implCode) > 0 {
				p.log.Debugf("impl code len (EIP-1167)=%d", len(implCode))
				for name, detail := range compiled {
					trimmedName := strings.TrimPrefix(name, "<stdin>:")
					if len(sc.Name) > 0 && sc.Name != trimmedName {
						continue
					}
					runtimeHex := detail.RuntimeCode
					if len(runtimeHex) <= 2 {
						continue
					}
					compiledRuntime, err3 := hexutil.Decode(runtimeHex)
					if err3 != nil || len(compiledRuntime) == 0 {
						continue
					}
					compiledRuntime = cutCodeMetadata(compiledRuntime)
					compiledRuntime = maskBytesAtPositions(compiledRuntime, combinePositions(detail.RuntimeLinkRefs, detail.RuntimeImmutableRefs))
					if len(implCode) >= len(compiledRuntime) {
						// mask with both link and immutable refs
						maskedOnChain := maskBytesAtPositions(implCode[:len(compiledRuntime)], combinePositions(detail.RuntimeLinkRefs, detail.RuntimeImmutableRefs))
						if bytes.Equal(compiledRuntime, maskedOnChain) {
							if len(sc.Name) == 0 {
								sc.Name = trimmedName
							}
							sc.Compiler = fmt.Sprintf("Solidity %s", detail.CompilerVersion)
							if len(detail.Abi) > 0 {
								sc.Abi = string(detail.Abi)
							}
							if len(detail.Metadata) > 0 {
								sc.Metadata = detail.Metadata
							}
							// capture bytecodes and link/immutable references for UI
							sc.CreationBytecode = detail.Code
							sc.RuntimeBytecode = detail.RuntimeCode
							sc.Version = detail.CompilerVersion
							sc.CreationLinkReferences = make([]types.LinkReferenceRange, len(detail.CreationLinkRefs))
							for i, r := range detail.CreationLinkRefs {
								sc.CreationLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
							}
							sc.RuntimeLinkReferences = make([]types.LinkReferenceRange, len(detail.RuntimeLinkRefs))
							for i, r := range detail.RuntimeLinkRefs {
								sc.RuntimeLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
							}
							sc.RuntimeImmutableReferences = make([]types.LinkReferenceRange, len(detail.RuntimeImmutableRefs))
							for i, r := range detail.RuntimeImmutableRefs {
								sc.RuntimeImmutableReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
							}
							now := hexutil.Uint64(uint64(time.Now().Unix()))
							sc.Validated = &now
							if err := p.pg.UpdateContractValidation(storeCtx(), sc); err != nil {
								p.log.Errorf("contract validation (EIP-1167 impl) failed due to db error; %s", err.Error())
								return err
							}
							p.log.Debugf("contract %s validated by EIP-1167 implementation runtime at %s with compiler %s", sc.Address.String(), implFromClone.Hex(), compilerPath)
							p.cache.EvictContract(&sc.Address)
							return nil
						}
					}
				}
			}
		}

		// Try custom proxy patterns if EIP-1167 didn't work
		if implFromCustom := tryExtractCustomProxyTarget(onChainCode); implFromCustom != (common.Address{}) {
			p.log.Debugf("detected custom proxy pattern; impl=%s", implFromCustom.Hex())
			if implCode, err2 := p.rpc.ContractCode(&implFromCustom); err2 == nil && len(implCode) > 0 {
				p.log.Debugf("impl code len (custom)=%d", len(implCode))
				for name, detail := range compiled {
					trimmedName := strings.TrimPrefix(name, "<stdin>:")
					if len(sc.Name) > 0 && sc.Name != trimmedName {
						continue
					}
					runtimeHex := detail.RuntimeCode
					if len(runtimeHex) <= 2 {
						continue
					}
					compiledRuntime, err3 := hexutil.Decode(runtimeHex)
					if err3 != nil || len(compiledRuntime) == 0 {
						continue
					}
					compiledRuntime = cutCodeMetadata(compiledRuntime)
					compiledRuntime = maskBytesAtPositions(compiledRuntime, combinePositions(detail.RuntimeLinkRefs, detail.RuntimeImmutableRefs))
					if len(implCode) >= len(compiledRuntime) {
						maskedOnChain := maskBytesAtPositions(implCode[:len(compiledRuntime)], combinePositions(detail.RuntimeLinkRefs, detail.RuntimeImmutableRefs))
						if bytes.Equal(compiledRuntime, maskedOnChain) {
							if len(sc.Name) == 0 {
								sc.Name = trimmedName
							}
							sc.Compiler = fmt.Sprintf("Solidity %s", detail.CompilerVersion)
							if len(detail.Abi) > 0 {
								sc.Abi = string(detail.Abi)
							}
							if len(detail.Metadata) > 0 {
								sc.Metadata = detail.Metadata
							}
							// capture bytecodes and link/immutable references for UI
							sc.CreationBytecode = detail.Code
							sc.RuntimeBytecode = detail.RuntimeCode
							sc.Version = detail.CompilerVersion
							sc.CreationLinkReferences = make([]types.LinkReferenceRange, len(detail.CreationLinkRefs))
							for i, r := range detail.CreationLinkRefs {
								sc.CreationLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
							}
							sc.RuntimeLinkReferences = make([]types.LinkReferenceRange, len(detail.RuntimeLinkRefs))
							for i, r := range detail.RuntimeLinkRefs {
								sc.RuntimeLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
							}
							sc.RuntimeImmutableReferences = make([]types.LinkReferenceRange, len(detail.RuntimeImmutableRefs))
							for i, r := range detail.RuntimeImmutableRefs {
								sc.RuntimeImmutableReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
							}
							now := hexutil.Uint64(uint64(time.Now().Unix()))
							sc.Validated = &now
							if err := p.pg.UpdateContractValidation(storeCtx(), sc); err != nil {
								p.log.Errorf("contract validation (custom proxy impl) failed due to db error; %s", err.Error())
								return err
							}
							p.log.Debugf("contract %s validated by custom proxy implementation runtime at %s with compiler %s", sc.Address.String(), implFromCustom.Hex(), compilerPath)
							p.cache.EvictContract(&sc.Address)
							return nil
						}
					}
				}
			}
		}

		// If runtime didn't match at the proxy address, attempt EIP-1967 implementation lookup (UUPS/Transparent)
		// EIP-1967 implementation slot
		implSlot := common.HexToHash("0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc")
		if slotBytes, err := p.rpc.StorageAt(&sc.Address, implSlot); err == nil && len(slotBytes) == 32 {
			// last 20 bytes are the implementation address
			implAddr := common.BytesToAddress(slotBytes[12:])
			if implAddr != (common.Address{}) {
				p.log.Debugf("EIP-1967 impl slot resolved: %s", implAddr.Hex())

				// For UUPS proxies, the implementation contract should match the source code
				// Try to verify the implementation contract directly
				if implCode, err2 := p.rpc.ContractCode(&implAddr); err2 == nil && len(implCode) > 0 {
					p.log.Debugf("impl code len (EIP-1967)=%d", len(implCode))

					// First try to match the implementation code with the source code
					for name, detail := range compiled {
						trimmedName := strings.TrimPrefix(name, "<stdin>:")
						if len(sc.Name) > 0 && sc.Name != trimmedName {
							continue
						}
						runtimeHex := detail.RuntimeCode
						if len(runtimeHex) <= 2 {
							continue
						}
						compiledRuntime, err3 := hexutil.Decode(runtimeHex)
						if err3 != nil || len(compiledRuntime) == 0 {
							continue
						}
						compiledRuntime = cutCodeMetadata(compiledRuntime)
						compiledRuntime = maskBytesAtPositions(compiledRuntime, combinePositions(detail.RuntimeLinkRefs, detail.RuntimeImmutableRefs))
						if len(implCode) >= len(compiledRuntime) {
							maskedOnChain := maskBytesAtPositions(implCode[:len(compiledRuntime)], combinePositions(detail.RuntimeLinkRefs, detail.RuntimeImmutableRefs))
							if bytes.Equal(compiledRuntime, maskedOnChain) {
								// Successfully matched implementation code
								if len(sc.Name) == 0 {
									sc.Name = trimmedName
								}
								sc.Compiler = fmt.Sprintf("Solidity %s", detail.CompilerVersion)
								if len(detail.Abi) > 0 {
									sc.Abi = string(detail.Abi)
								}
								if len(detail.Metadata) > 0 {
									sc.Metadata = detail.Metadata
								}
								// capture bytecodes and link/immutable references for UI
								sc.CreationBytecode = detail.Code
								sc.RuntimeBytecode = detail.RuntimeCode
								sc.Version = detail.CompilerVersion

								sc.CreationLinkReferences = make([]types.LinkReferenceRange, len(detail.CreationLinkRefs))
								for i, r := range detail.CreationLinkRefs {
									sc.CreationLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
								}
								sc.RuntimeLinkReferences = make([]types.LinkReferenceRange, len(detail.RuntimeLinkRefs))
								for i, r := range detail.RuntimeLinkRefs {
									sc.RuntimeLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
								}
								sc.RuntimeImmutableReferences = make([]types.LinkReferenceRange, len(detail.RuntimeImmutableRefs))
								for i, r := range detail.RuntimeImmutableRefs {
									sc.RuntimeImmutableReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
								}
								now := hexutil.Uint64(uint64(time.Now().Unix()))
								sc.Validated = &now
								if err := p.pg.UpdateContractValidation(storeCtx(), sc); err != nil {
									p.log.Errorf("contract validation (proxy impl) failed due to db error; %s", err.Error())
									return err
								}
								p.log.Debugf("contract %s [%s] validated by implementation runtime bytecode at %s with compiler %s", sc.Address.String(), name, implAddr.Hex(), compilerPath)
								p.cache.EvictContract(&sc.Address)
								return nil
							}
						}
					}
				}
				if implCode, err2 := p.rpc.ContractCode(&implAddr); err2 == nil && len(implCode) > 0 {
					p.log.Debugf("impl code len (EIP-1967)=%d", len(implCode))
					for name, detail := range compiled {
						trimmedName := strings.TrimPrefix(name, "<stdin>:")
						if len(sc.Name) > 0 && sc.Name != trimmedName {
							continue
						}
						runtimeHex := detail.RuntimeCode
						if len(runtimeHex) <= 2 {
							continue
						}
						compiledRuntime, err3 := hexutil.Decode(runtimeHex)
						if err3 != nil || len(compiledRuntime) == 0 {
							continue
						}
						compiledRuntime = cutCodeMetadata(compiledRuntime)
						compiledRuntime = maskBytesAtPositions(compiledRuntime, combinePositions(detail.RuntimeLinkRefs, detail.RuntimeImmutableRefs))
						if len(implCode) >= len(compiledRuntime) {
							maskedOnChain := maskBytesAtPositions(implCode[:len(compiledRuntime)], detail.RuntimeLinkRefs)
							if bytes.Equal(compiledRuntime, maskedOnChain) {
								if len(sc.Name) == 0 {
									sc.Name = trimmedName
								}
								sc.Compiler = fmt.Sprintf("Solidity %s", detail.CompilerVersion)
								if len(detail.Abi) > 0 {
									sc.Abi = string(detail.Abi)
								}
								if len(detail.Metadata) > 0 {
									sc.Metadata = detail.Metadata
								}
								// capture bytecodes and link/immutable references for UI
								sc.CreationBytecode = detail.Code
								sc.RuntimeBytecode = detail.RuntimeCode
								sc.Version = detail.CompilerVersion
								sc.CreationLinkReferences = make([]types.LinkReferenceRange, len(detail.CreationLinkRefs))
								for i, r := range detail.CreationLinkRefs {
									sc.CreationLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
								}
								sc.RuntimeLinkReferences = make([]types.LinkReferenceRange, len(detail.RuntimeLinkRefs))
								for i, r := range detail.RuntimeLinkRefs {
									sc.RuntimeLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
								}
								sc.RuntimeImmutableReferences = make([]types.LinkReferenceRange, len(detail.RuntimeImmutableRefs))
								for i, r := range detail.RuntimeImmutableRefs {
									sc.RuntimeImmutableReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
								}
								now := hexutil.Uint64(uint64(time.Now().Unix()))
								sc.Validated = &now
								if err := p.pg.UpdateContractValidation(storeCtx(), sc); err != nil {
									p.log.Errorf("contract validation (proxy impl) failed due to db error; %s", err.Error())
									return err
								}
								p.log.Debugf("contract %s [%s] validated by implementation runtime bytecode at %s with compiler %s", sc.Address.String(), name, implAddr.Hex(), compilerPath)
								p.cache.EvictContract(&sc.Address)
								return nil
							}

							// Try EIP-1967 beacon pattern: read beacon slot, then call implementation() on the beacon
							beaconSlot := common.HexToHash("0xa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50")
							if slotBytes, err := p.rpc.StorageAt(&sc.Address, beaconSlot); err == nil && len(slotBytes) == 32 {
								beaconAddr := common.BytesToAddress(slotBytes[12:])
								if beaconAddr != (common.Address{}) {
									p.log.Debugf("EIP-1967 beacon slot resolved: %s", beaconAddr.Hex())
									// call implementation() on the beacon
									selector := []byte{0x5c, 0x60, 0xda, 0x1b}
									if ret, err := p.rpc.Call(&beaconAddr, selector); err == nil && len(ret) >= 32 {
										implAddr := common.BytesToAddress(ret[len(ret)-20:])
										if implAddr != (common.Address{}) {
											p.log.Debugf("beacon implementation resolved: %s", implAddr.Hex())
											if implCode, err2 := p.rpc.ContractCode(&implAddr); err2 == nil && len(implCode) > 0 {
												p.log.Debugf("impl code len (beacon)=%d", len(implCode))
												for name, detail := range compiled {
													trimmedName := strings.TrimPrefix(name, "<stdin>:")
													if len(sc.Name) > 0 && sc.Name != trimmedName {
														continue
													}
													runtimeHex := detail.RuntimeCode
													if len(runtimeHex) <= 2 {
														continue
													}
													compiledRuntime, err3 := hexutil.Decode(runtimeHex)
													if err3 != nil || len(compiledRuntime) == 0 {
														continue
													}
													compiledRuntime = cutCodeMetadata(compiledRuntime)
													compiledRuntime = maskBytesAtPositions(compiledRuntime, combinePositions(detail.RuntimeLinkRefs, detail.RuntimeImmutableRefs))
													if len(implCode) >= len(compiledRuntime) {
														maskedOnChain := maskBytesAtPositions(implCode[:len(compiledRuntime)], detail.RuntimeLinkRefs)
														if bytes.Equal(compiledRuntime, maskedOnChain) {
															if len(sc.Name) == 0 {
																sc.Name = trimmedName
															}
															sc.Compiler = fmt.Sprintf("Solidity %s", detail.CompilerVersion)
															if len(detail.Abi) > 0 {
																sc.Abi = string(detail.Abi)
															}
															if len(detail.Metadata) > 0 {
																sc.Metadata = detail.Metadata
															}
															now := hexutil.Uint64(uint64(time.Now().Unix()))
															sc.Validated = &now
															if err := p.pg.UpdateContractValidation(storeCtx(), sc); err != nil {
																p.log.Errorf("contract validation (beacon impl) failed due to db error; %s", err.Error())
																return err
															}
															p.log.Debugf("contract %s validated by beacon implementation runtime at %s with compiler %s", sc.Address.String(), implAddr.Hex(), compilerPath)
															p.cache.EvictContract(&sc.Address)
															return nil
														}
													}
												}
											}
										}
									}
								}
							}
						}

						// If still no match, try a few alternative compiler settings heuristics
						type altCfg struct {
							optimized bool
							runs      int32
							evm       string
							viaIR     bool
						}
						candidates := []altCfg{}

						// toggle optimizer
						candidates = append(candidates, altCfg{optimized: !sc.IsOptimized, runs: sc.OptimizeRuns, evm: sc.EvmVersion, viaIR: sc.ViaIR})

						// try viaIR toggle
						candidates = append(candidates, altCfg{optimized: sc.IsOptimized, runs: sc.OptimizeRuns, evm: sc.EvmVersion, viaIR: !sc.ViaIR})

						// try different optimization runs
						if sc.OptimizeRuns != 200 {
							candidates = append(candidates, altCfg{optimized: sc.IsOptimized, runs: 200, evm: sc.EvmVersion, viaIR: sc.ViaIR})
						}
						if sc.OptimizeRuns != 1 {
							candidates = append(candidates, altCfg{optimized: sc.IsOptimized, runs: 1, evm: sc.EvmVersion, viaIR: sc.ViaIR})
						}

						// try common evm versions if not set
						if sc.EvmVersion == "" {
							for _, ev := range []string{"paris", "shanghai", "london", "berlin", "istanbul"} {
								candidates = append(candidates, altCfg{optimized: sc.IsOptimized, runs: sc.OptimizeRuns, evm: ev, viaIR: sc.ViaIR})
							}
						} else {
							// try other common evm versions even if one is set
							commonEvmVersions := []string{"paris", "shanghai", "london", "berlin", "istanbul"}
							for _, ev := range commonEvmVersions {
								if ev != sc.EvmVersion {
									candidates = append(candidates, altCfg{optimized: sc.IsOptimized, runs: sc.OptimizeRuns, evm: ev, viaIR: sc.ViaIR})
								}
							}
						}

						for _, cfgAlt := range candidates {
							altCompiled, cerr := compileSolidityStandardJSON(compilerPath, sc.SourceCode, cfgAlt.optimized, cfgAlt.runs, cfgAlt.evm, cfgAlt.viaIR)
							if cerr != nil || len(altCompiled) == 0 {
								continue
							}
							for name2, detail2 := range altCompiled {
								trimmedName2 := strings.TrimPrefix(name2, "<stdin>:")
								if len(sc.Name) > 0 && sc.Name != trimmedName2 {
									continue
								}
								if len(detail2.RuntimeCode) <= 2 {
									continue
								}
								cr2, derr := hexutil.Decode(detail2.RuntimeCode)
								if derr != nil || len(cr2) == 0 {
									continue
								}
								cr2 = cutCodeMetadata(cr2)
								cr2 = maskBytesAtPositions(cr2, detail2.RuntimeLinkRefs)
								if len(implCode) >= len(cr2) {
									masked := maskBytesAtPositions(implCode[:len(cr2)], detail2.RuntimeLinkRefs)
									if bytes.Equal(cr2, masked) {
										if len(sc.Name) == 0 {
											sc.Name = trimmedName2
										}
										sc.Compiler = fmt.Sprintf("Solidity %s", detail2.CompilerVersion)
										if len(detail2.Abi) > 0 {
											sc.Abi = string(detail2.Abi)
										}
										if len(detail2.Metadata) > 0 {
											sc.Metadata = detail2.Metadata
										}
										// capture bytecodes and link/immutable references for UI
										sc.CreationBytecode = detail2.Code
										sc.RuntimeBytecode = detail2.RuntimeCode
										sc.CreationLinkReferences = make([]types.LinkReferenceRange, len(detail2.CreationLinkRefs))
										for i, r := range detail2.CreationLinkRefs {
											sc.CreationLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
										}
										sc.RuntimeLinkReferences = make([]types.LinkReferenceRange, len(detail2.RuntimeLinkRefs))
										for i, r := range detail2.RuntimeLinkRefs {
											sc.RuntimeLinkReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
										}
										sc.RuntimeImmutableReferences = make([]types.LinkReferenceRange, len(detail2.RuntimeImmutableRefs))
										for i, r := range detail2.RuntimeImmutableRefs {
											sc.RuntimeImmutableReferences[i] = types.LinkReferenceRange{Start: int32(r.Start), Length: int32(r.Length)}
										}
										// persist the actual settings that matched
										sc.IsOptimized = cfgAlt.optimized
										sc.OptimizeRuns = cfgAlt.runs
										sc.EvmVersion = cfgAlt.evm
										sc.ViaIR = cfgAlt.viaIR
										sc.Version = detail2.CompilerVersion

										now := hexutil.Uint64(uint64(time.Now().Unix()))
										sc.Validated = &now
										if err := p.pg.UpdateContractValidation(storeCtx(), sc); err != nil {
											p.log.Errorf("contract validation (proxy impl alt) failed due to db error; %s", err.Error())
											return err
										}
										p.log.Debugf("contract %s [%s] validated by implementation runtime with adjusted settings (opt=%v runs=%d evm=%s viaIR=%v)", sc.Address.String(), name2, cfgAlt.optimized, cfgAlt.runs, cfgAlt.evm, cfgAlt.viaIR)
										p.cache.EvictContract(&sc.Address)
										return nil
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// validation fails
	return fmt.Errorf("contract source code does not match with the deployed byte code")
}

// VerifyProxyContract verifies a proxy address flow.
// - Detect proxy (EIP-1167, EIP-1967, Beacon)
// - If parent not verified: return parent address and instruction message
// - If parent verified: link proxy -> implementation, copy ABI/metadata, mark proxy validated
func (p *proxy) VerifyProxyContract(addr *common.Address) (*types.Contract, *common.Address, bool, string, error) {
	if addr == nil {
		return nil, nil, false, "no address provided", fmt.Errorf("no address provided")
	}

	// Load contract from DB
	con, err := p.pg.Contract(storeCtx(), addr)
	if err != nil {
		return nil, nil, false, "failed to load contract", err
	}
	if con == nil {
		return nil, nil, false, "contract not found", fmt.Errorf("contract not found")
	}

	// Detect proxy and implementation
	onChainCode, _ := p.rpc.ContractCode(addr)
	implAddr := common.Address{}
	proxyType := ""
	if len(onChainCode) > 0 {
		if impl := tryExtractEIP1167Target(onChainCode); impl != (common.Address{}) {
			implAddr = impl
			proxyType = "EIP-1167"
		}
	}
	if implAddr == (common.Address{}) {
		implSlot := common.HexToHash("0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc")
		if slotBytes, err := p.rpc.StorageAt(addr, implSlot); err == nil && len(slotBytes) == 32 {
			a := common.BytesToAddress(slotBytes[12:])
			if a != (common.Address{}) {
				implAddr = a
				proxyType = "EIP-1967"
			}
		}
	}
	if implAddr == (common.Address{}) {
		beaconSlot := common.HexToHash("0xa3f0ad74e5423aebfd80d3ef4346578335a9a72aeaee59ff6cb3582b35133d50")
		if slotBytes, err := p.rpc.StorageAt(addr, beaconSlot); err == nil && len(slotBytes) == 32 {
			beaconAddr := common.BytesToAddress(slotBytes[12:])
			if beaconAddr != (common.Address{}) {
				selector := []byte{0x5c, 0x60, 0xda, 0x1b}
				if ret, err := p.rpc.Call(&beaconAddr, selector); err == nil && len(ret) >= 32 {
					impl := common.BytesToAddress(ret[len(ret)-20:])
					if impl != (common.Address{}) {
						implAddr = impl
						proxyType = "Beacon(EIP-1967)"
					}
				}
			}
		}
	}

	if implAddr == (common.Address{}) {
		return con, nil, false, "A corresponding implementation contract was unfortunately not detected for the proxy address", nil
	}

	implCon, derr := p.pg.Contract(storeCtx(), &implAddr)
	if derr != nil {
		return con, &implAddr, false, "failed to load implementation contract", derr
	}
	if implCon == nil || implCon.Validated == nil {
		msg := fmt.Sprintf("proxy detected (%s). Please verify the parent implementation first: %s", proxyType, implAddr.Hex())
		return con, &implAddr, false, msg, nil
	}

	// Parent is verified: link and mark proxy verified, inherit ABI/metadata if missing
	con.IsProxy = true
	con.ProxyType = proxyType
	con.ImplementationAddress = implAddr
	con.Name = implCon.Name
	con.Version = implCon.Version
	con.License = implCon.License
	con.Compiler = implCon.Compiler
	con.IsOptimized = implCon.IsOptimized
	con.OptimizeRuns = implCon.OptimizeRuns
	con.EvmVersion = implCon.EvmVersion
	con.ViaIR = implCon.ViaIR
	// If the source code is not set, we can try to copy it from the implementation
	if con.SourceCode == "" && implCon.SourceCode != "" {
		con.SourceCode = implCon.SourceCode
		if implCon.SourceCodeHash != nil {
			con.SourceCodeHash = implCon.SourceCodeHash
		}
	}

	if con.Abi == "" {
		con.Abi = implCon.Abi
	}

	if con.Metadata == "" {
		con.Metadata = implCon.Metadata
	}

	if con.CompilerVersion != "" {
		con.Version = con.CompilerVersion
	}

	now := hexutil.Uint64(uint64(time.Now().Unix()))
	con.Validated = &now
	if err := p.pg.UpdateContractValidation(storeCtx(), con); err != nil {
		return con, &implAddr, false, "failed to update proxy contract", err
	}
	p.cache.EvictContract(&con.Address)
	msg := fmt.Sprintf("proxy linked to implementation %s (%s) and marked verified", implAddr.Hex(), proxyType)
	return con, &implAddr, true, msg, nil
}

// StoreContract adds new contract into the repository.
func (p *proxy) StoreContract(con *types.Contract) error {
	// is the a known contract which will be updated?
	isUpdate, _ := p.pg.IsContractKnown(storeCtx(), &con.Address)

	// do the add/update op
	if err := p.pg.AddContract(storeCtx(), con); err != nil {
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

// isUUPSProxy checks if a contract is a UUPS proxy by looking for EIP-1967 implementation slot
func isUUPSProxy(contractAddr common.Address, rpc interface{}) bool {
	// Check EIP-1967 implementation slot
	implSlot := common.HexToHash("0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc")

	// Type assertion to get the RPC interface
	if rpcBridge, ok := rpc.(interface {
		StorageAt(*common.Address, common.Hash) ([]byte, error)
	}); ok {
		if slotBytes, err := rpcBridge.StorageAt(&contractAddr, implSlot); err == nil && len(slotBytes) == 32 {
			implAddr := common.BytesToAddress(slotBytes[12:])
			if implAddr != (common.Address{}) {
				return true
			}
		}
	}
	return false
}

// getUUPSImplementation extracts the implementation address from a UUPS proxy contract
func getUUPSImplementation(proxyAddr common.Address, rpc interface{}) (common.Address, error) {
	implSlot := common.HexToHash("0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc")

	if rpcBridge, ok := rpc.(interface {
		StorageAt(*common.Address, common.Hash) ([]byte, error)
	}); ok {
		slotBytes, err := rpcBridge.StorageAt(&proxyAddr, implSlot)
		if err != nil {
			return common.Address{}, err
		}
		if len(slotBytes) != 32 {
			return common.Address{}, fmt.Errorf("invalid slot data length")
		}
		return common.BytesToAddress(slotBytes[12:]), nil
	}
	return common.Address{}, fmt.Errorf("RPC interface not available")
}

// verifyUUPSProxyContract specifically handles UUPS proxy contract verification
func verifyUUPSProxyContract(sc *types.Contract, compiled map[string]compiledArtifact, rpc interface{}, log interface{}) error {
	// Check if this is a UUPS proxy by looking for specific patterns in source code
	if !strings.Contains(sc.SourceCode, "UUPSUpgradeable") && !strings.Contains(sc.SourceCode, "_authorizeUpgrade") {
		return fmt.Errorf("not a UUPS proxy contract")
	}

	// Get implementation address from EIP-1967 slot
	implAddr, err := getUUPSImplementation(sc.Address, rpc)
	if err != nil {
		return fmt.Errorf("failed to get UUPS implementation: %w", err)
	}

	// Type assertion for logging
	if logger, ok := log.(interface{ Debugf(string, ...interface{}) }); ok {
		logger.Debugf("UUPS implementation address: %s", implAddr.Hex())
	}

	// Get implementation contract bytecode
	if rpcBridge, ok := rpc.(interface {
		ContractCode(*common.Address) ([]byte, error)
	}); ok {
		implCode, err := rpcBridge.ContractCode(&implAddr)
		if err != nil || len(implCode) == 0 {
			return fmt.Errorf("failed to get implementation code: %w", err)
		}

		if logger, ok := log.(interface{ Debugf(string, ...interface{}) }); ok {
			logger.Debugf("implementation code length: %d", len(implCode))
		}

		// Try to match compiled source code with implementation bytecode
		for name, detail := range compiled {
			trimmedName := strings.TrimPrefix(name, "<stdin>:")
			if len(sc.Name) > 0 && sc.Name != trimmedName {
				continue
			}

			runtimeHex := detail.RuntimeCode
			if len(runtimeHex) <= 2 {
				continue
			}

			compiledRuntime, err := hexutil.Decode(runtimeHex)
			if err != nil || len(compiledRuntime) == 0 {
				continue
			}

			// Remove metadata and mask link references
			compiledRuntime = cutCodeMetadata(compiledRuntime)
			compiledRuntime = maskBytesAtPositions(compiledRuntime, combinePositions(detail.RuntimeLinkRefs, detail.RuntimeImmutableRefs))

			if len(implCode) >= len(compiledRuntime) {
				maskedOnChain := maskBytesAtPositions(implCode[:len(compiledRuntime)], detail.RuntimeLinkRefs)
				if bytes.Equal(compiledRuntime, maskedOnChain) {
					// Successfully matched UUPS implementation!
					return nil
				}
			}
		}
	}

	return fmt.Errorf("no matching implementation found")
}

// parseSolidityPragmas scans source for pragma solidity and returns raw constraints
func parseSolidityPragmas(source string) []string {
	lines := strings.Split(source, "\n")
	out := make([]string, 0, 4)
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "pragma solidity") {
			out = append(out, trim)
		}
	}
	return out
}

// candidateSolcVersions builds a list of solc versions to try based on pragmas; falls back to common 0.8.x
func candidateSolcVersions(source string, preferred string) []string {
	// Popular 0.8.x versions in descending recency for better hit-rate
	common := []string{
		"0.8.26", "0.8.25", "0.8.24", "0.8.23", "0.8.22", "0.8.21", "0.8.20",
		"0.8.19", "0.8.18", "0.8.17", "0.8.16", "0.8.15", "0.8.14", "0.8.13",
		"0.8.12", "0.8.11", "0.8.10", "0.8.9", "0.8.8", "0.8.7", "0.8.6",
	}
	// If preferred provided, place it first
	seen := map[string]bool{}
	order := make([]string, 0, len(common)+4)
	if preferred != "" {
		order = append(order, preferred)
		seen[preferred] = true
	}
	// Try to infer from pragmas (simple heuristics)
	for _, pg := range parseSolidityPragmas(source) {
		// examples: "pragma solidity ^0.8.9;" or ">=0.8.0 <0.9.0"
		if strings.Contains(pg, "0.8.") {
			// collect all 0.8.x from common that satisfy a rough bound
			// If caret found like ^0.8.9 -> require >= that patch
			minPatch := -1
			idx := strings.Index(pg, "0.8.")
			if idx >= 0 && idx+4 < len(pg) {
				// parse single digit/2-digit patch
				patchStr := ""
				for i := idx + 4; i < len(pg); i++ {
					ch := pg[i]
					if ch >= '0' && ch <= '9' {
						patchStr += string(ch)
					} else {
						break
					}
				}
				if patchStr != "" {
					// naive atoi
					n := 0
					for i := 0; i < len(patchStr); i++ {
						n = n*10 + int(patchStr[i]-'0')
					}
					minPatch = n
				}
			}
			for _, v := range common {
				if seen[v] {
					continue
				}
				if minPatch >= 0 {
					// v like 0.8.xx -> ensure >= minPatch
					parts := strings.Split(v, ".")
					if len(parts) == 3 {
						patchStr := parts[2]
						pn := 0
						for i := 0; i < len(patchStr); i++ {
							pn = pn*10 + int(patchStr[i]-'0')
						}
						if pn < minPatch {
							continue
						}
					}
				}
				order = append(order, v)
				seen[v] = true
			}
		}
	}
	// fill remaining
	for _, v := range common {
		if !seen[v] {
			order = append(order, v)
			seen[v] = true
		}
	}
	return order
}
