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
    "net/http"
    "ncogearthchain-api-graphql/internal/types"
    "os/exec"
    "path"
    "regexp"
    "strings"
    "time"

    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/common/compiler"
    "github.com/ethereum/go-ethereum/common/hexutil"
    "compress/gzip"
)

// extractPackageAndRest splits an import key like "@openzeppelin/contracts/access/Ownable.sol"
// into package name "@openzeppelin/contracts" and rest "access/Ownable.sol".
func extractPackageAndRest(key string) (string, string) {
    k := strings.TrimPrefix(key, "/")
    if !strings.Contains(k, "/") {
        return k, ""
    }
    parts := strings.Split(k, "/")
    if len(parts) == 0 {
        return k, ""
    }
    // scoped pkg
    if strings.HasPrefix(k, "@") && len(parts) >= 2 {
        pkg := parts[0] + "/" + parts[1]
        rest := strings.Join(parts[2:], "/")
        return pkg, rest
    }
    // unscoped
    pkg := parts[0]
    rest := strings.Join(parts[1:], "/")
    return pkg, rest
}

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

// compareContractCode compares provided compiled code with the transaction input.
func compareContractCode(tx *types.Transaction, code string) (bool, error) {
	// decode the detail into byte array
	bc, err := hexutil.Decode(code)
	if err != nil {
		return false, err
	}
	if len(bc) == 0 {
		return false, nil
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

// compiledArtifact represents a minimal subset of compiler output we need
type compiledArtifact struct {
	Name            string
	Code            string // creation bytecode hex (0x...)
	RuntimeCode     string // deployed/runtime bytecode hex (0x...)
	Abi             json.RawMessage
	CompilerVersion string
    CreationLinkRefs []linkPos
    RuntimeLinkRefs  []linkPos
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

// detectPackages scans source for common package import roots.
func detectPackages(source string) []string {
    pkgs := map[string]bool{}
    matches := importRegexp.FindAllStringSubmatch(source, -1)
    for _, m := range matches {
        var imp string
        for i := 1; i < len(m); i++ {
            if m[i] != "" {
                imp = m[i]
                break
            }
        }
        if imp == "" || strings.HasPrefix(imp, ".") || strings.HasPrefix(imp, "http") {
            continue
        }
        pkg, _ := extractPackageAndRest(imp)
        if pkg != "" {
            pkgs[pkg] = true
        }
    }
    
    // Also detect packages mentioned in comments or documentation
    // This helps catch packages that might be referenced but not imported
    additionalPackages := detectPackagesFromComments(source)
    for _, pkg := range additionalPackages {
        pkgs[pkg] = true
    }
    
    out := make([]string, 0, len(pkgs))
    for k := range pkgs {
        out = append(out, k)
    }
    return out
}

// detectPackagesFromComments scans comments and documentation for package references
func detectPackagesFromComments(source string) []string {
    var packages []string
    
    // Common package patterns in comments
    commentPatterns := []string{
        `@openzeppelin/contracts`,
        `openzeppelin-solidity`,
        `@chainlink/contracts`,
        `chainlink`,
        `@uniswap/`,
        `uniswap`,
        `@aave/`,
        `aave`,
        `@compound-finance/`,
        `compound`,
        `@synthetixio/`,
        `synthetix`,
        `@balancer-labs/`,
        `balancer`,
        `@dapphub/ds-`,
        `ds-`,
        `@gnosis/`,
        `gnosis`,
        `@openzeppelin/upgrades`,
        `@openzeppelin/hardhat-upgrades`,
        `@openzeppelin/test-helpers`,
        `@openzeppelin/contracts-upgradeable`,
        `@openzeppelin/contracts-ethereum-package`,
        `@openzeppelin/contracts-math`,
        `@openzeppelin/contracts-drafts`,
        `@openzeppelin/contracts-mocks`,
        `@openzeppelin/contracts-examples`,
        `@openzeppelin/contracts-wizard`,
        `@openzeppelin/contracts-utils`,
        `@openzeppelin/contracts-token`,
        `@openzeppelin/contracts-security`,
        `@openzeppelin/contracts-proxy`,
        `@openzeppelin/contracts-governance`,
        `@openzeppelin/contracts-finance`,
        `@openzeppelin/contracts-crosschain`,
        `@openzeppelin/contracts-build`,
        `@openzeppelin/contracts-assets`,
        `@openzeppelin/contracts-accounts`,
        `@openzeppelin/contracts-access`,
        `@openzeppelin/contracts-utils`,
        `@openzeppelin/contracts-token`,
        `@openzeppelin/contracts-security`,
        `@openzeppelin/contracts-proxy`,
        `@openzeppelin/contracts-governance`,
        `@openzeppelin/contracts-finance`,
        `@openzeppelin/contracts-crosschain`,
        `@openzeppelin/contracts-build`,
        `@openzeppelin/contracts-assets`,
        `@openzeppelin/contracts-accounts`,
        `@openzeppelin/contracts-access`,
    }
    
    for _, pattern := range commentPatterns {
        if strings.Contains(source, pattern) {
            // Extract the base package name
            if strings.HasPrefix(pattern, "@") {
                parts := strings.Split(pattern, "/")
                if len(parts) >= 2 {
                    packages = append(packages, parts[0]+"/"+parts[1])
                }
            } else {
                packages = append(packages, pattern)
            }
        }
    }
    
    return packages
}

// generateVersionPinAttempts returns a list of pin maps to try for detected packages.
// Supports popular packages with curated historical version sets.
func generateVersionPinAttempts(detected []string) []map[string]string {
    var attempts []map[string]string
    
    // OpenZeppelin packages - try multiple versions
    var hasOZ bool
    for _, d := range detected {
        if strings.HasPrefix(d, "@openzeppelin/") || d == "openzeppelin-solidity" {
            hasOZ = true
            break
        }
    }
    
    if hasOZ {
        // OpenZeppelin v5.x versions (newest first)
        ozV5 := []string{"5.0.1", "5.0.0"}
        // OpenZeppelin v4.x versions
        ozV4 := []string{"4.9.6", "4.9.5", "4.9.4", "4.9.3", "4.9.2", "4.9.1", "4.9.0", 
                         "4.8.3", "4.8.2", "4.8.1", "4.8.0", "4.7.3", "4.7.2", "4.7.1", "4.7.0",
                         "4.6.0", "4.5.0", "4.4.2", "4.4.1", "4.4.0", "4.3.3", "4.3.2", "4.3.1", "4.3.0",
                         "4.2.0", "4.1.0", "4.0.0"}
        // OpenZeppelin v3.x versions
        ozV3 := []string{"3.4.2", "3.4.1", "3.4.0", "3.3.0", "3.2.0", "3.1.0", "3.0.0"}
        // OpenZeppelin v2.x versions
        ozV2 := []string{"2.5.1", "2.5.0", "2.4.0", "2.3.0", "2.2.0", "2.1.0", "2.0.0"}
        
        // Try v5.x first (newest)
        for _, v := range ozV5 {
            m := map[string]string{
                "@openzeppelin/contracts": v,
                "openzeppelin-solidity":  v,
            }
            attempts = append(attempts, m)
        }
        
        // Then try v4.x
        for _, v := range ozV4 {
            m := map[string]string{
                "@openzeppelin/contracts": v,
                "openzeppelin-solidity":  v,
            }
            attempts = append(attempts, m)
        }
        
        // Then try v3.x
        for _, v := range ozV3 {
            m := map[string]string{
                "@openzeppelin/contracts": v,
                "openzeppelin-solidity":  v,
            }
            attempts = append(attempts, m)
        }
        
        // Finally try v2.x
        for _, v := range ozV2 {
            m := map[string]string{
                "@openzeppelin/contracts": v,
                "openzeppelin-solidity":  v,
            }
            attempts = append(attempts, m)
        }
    }
    
    // Chainlink packages
    var hasChainlink bool
    for _, d := range detected {
        if strings.HasPrefix(d, "@chainlink/") || d == "chainlink" {
            hasChainlink = true
            break
        }
    }
    
    if hasChainlink {
        chainlinkVersions := []string{"0.0.16", "0.0.15", "0.0.14", "0.0.13", "0.0.12", "0.0.11", "0.0.10"}
        for _, v := range chainlinkVersions {
            m := map[string]string{
                "@chainlink/contracts": v,
                "chainlink":            v,
            }
            attempts = append(attempts, m)
        }
    }
    
    // Uniswap packages
    var hasUniswap bool
    for _, d := range detected {
        if strings.HasPrefix(d, "@uniswap/") || strings.Contains(d, "uniswap") {
            hasUniswap = true
            break
        }
    }
    
    if hasUniswap {
        uniswapVersions := []string{"4.0.0", "3.1.0", "3.0.0", "2.0.0"}
        for _, v := range uniswapVersions {
            m := map[string]string{
                "@uniswap/v4-core":      v,
                "@uniswap/v4-periphery": v,
                "@uniswap/v3-core":      v,
                "@uniswap/v3-periphery": v,
                "@uniswap/v2-core":      v,
                "@uniswap/v2-periphery": v,
                "uniswap-v2-core":       v,
                "uniswap-v2-periphery":  v,
            }
            attempts = append(attempts, m)
        }
    }
    
    // Aave packages
    var hasAave bool
    for _, d := range detected {
        if strings.HasPrefix(d, "@aave/") || strings.Contains(d, "aave") {
            hasAave = true
            break
        }
    }
    
    if hasAave {
        aaveVersions := []string{"1.19.1", "1.18.2", "1.17.2", "1.16.2", "1.15.0", "1.14.0"}
        for _, v := range aaveVersions {
            m := map[string]string{
                "@aave/core-v3":         v,
                "@aave/core-v2":         v,
                "@aave/protocol-commons": v,
                "aave-address-book":     v,
            }
            attempts = append(attempts, m)
        }
    }
    
    // Compound packages
    var hasCompound bool
    for _, d := range detected {
        if strings.HasPrefix(d, "@compound-finance/") || strings.Contains(d, "compound") {
            hasCompound = true
            break
        }
    }
    
    if hasCompound {
        compoundVersions := []string{"3.1.0", "3.0.0", "2.8.1", "2.8.0"}
        for _, v := range compoundVersions {
            m := map[string]string{
                "@compound-finance/compound-protocol": v,
                "compound-protocol":                   v,
            }
            attempts = append(attempts, m)
        }
    }
    
    // Synthetix packages
    var hasSynthetix bool
    for _, d := range detected {
        if strings.HasPrefix(d, "@synthetixio/") || strings.Contains(d, "synthetix") {
            hasSynthetix = true
            break
        }
    }
    
    if hasSynthetix {
        synthetixVersions := []string{"2.86.0", "2.85.0", "2.84.0", "2.83.0"}
        for _, v := range synthetixVersions {
            m := map[string]string{
                "@synthetixio/contracts": v,
                "synthetix":               v,
            }
            attempts = append(attempts, m)
        }
    }
    
    // Balancer packages
    var hasBalancer bool
    for _, d := range detected {
        if strings.HasPrefix(d, "@balancer-labs/") || strings.Contains(d, "balancer") {
            hasBalancer = true
            break
        }
    }
    
    if hasBalancer {
        balancerVersions := []string{"0.4.0", "0.3.0", "0.2.0", "0.1.0"}
        for _, v := range balancerVersions {
            m := map[string]string{
                "@balancer-labs/v2-vault":      v,
                "@balancer-labs/v2-pool-utils": v,
                "@balancer-labs/v2-pool-weighted": v,
                "balancer-v2-vault":            v,
            }
            attempts = append(attempts, m)
        }
    }
    
    // DappHub packages (DS-Token, DS-Math, etc.)
    var hasDappHub bool
    for _, d := range detected {
        if strings.HasPrefix(d, "@dapphub/") || strings.HasPrefix(d, "ds-") {
            hasDappHub = true
            break
        }
    }
    
    if hasDappHub {
        dappHubVersions := []string{"2.0.0", "1.0.0"}
        for _, v := range dappHubVersions {
            m := map[string]string{
                "@dapphub/ds-token":  v,
                "@dapphub/ds-math":   v,
                "@dapphub/ds-auth":   v,
                "@dapphub/ds-stop":   v,
                "@dapphub/ds-note":   v,
                "@dapphub/ds-proxy":  v,
                "@dapphub/ds-pause":  v,
                "@dapphub/ds-chief":  v,
                "@dapphub/ds-guard":  v,
                "@dapphub/ds-ward":   v,
                "@dapphub/ds-value":  v,
                "@dapphub/ds-spell":  v,
                "@dapphub/ds-roles":  v,
                "@dapphub/ds-test":   v,
                "@dapphub/ds-token":  v,
                "@dapphub/ds-math":   v,
                "@dapphub/ds-auth":   v,
                "@dapphub/ds-stop":   v,
                "@dapphub/ds-note":   v,
                "@dapphub/ds-proxy":  v,
                "@dapphub/ds-pause":  v,
                "@dapphub/ds-chief":  v,
                "@dapphub/ds-guard":  v,
                "@dapphub/ds-ward":   v,
                "@dapphub/ds-value":  v,
                "@dapphub/ds-spell":  v,
                "@dapphub/ds-roles":  v,
                "@dapphub/ds-test":   v,
            }
            attempts = append(attempts, m)
        }
    }
    
    // Gnosis packages
    var hasGnosis bool
    for _, d := range detected {
        if strings.HasPrefix(d, "@gnosis/") || strings.Contains(d, "gnosis") {
            hasGnosis = true
            break
        }
    }
    
    if hasGnosis {
        gnosisVersions := []string{"1.4.0", "1.3.0", "1.2.0", "1.1.0", "1.0.0"}
        for _, v := range gnosisVersions {
            m := map[string]string{
                "@gnosis/safe-contracts": v,
                "@gnosis/safe-core":     v,
                "@gnosis/safe-deployments": v,
                "gnosis-safe":           v,
            }
            attempts = append(attempts, m)
        }
    }
    
    // MakerDAO packages
    var hasMaker bool
    for _, d := range detected {
        if strings.HasPrefix(d, "@makerdao/") || strings.Contains(d, "dss") || strings.Contains(d, "maker") {
            hasMaker = true
            break
        }
    }
    
    if hasMaker {
        makerVersions := []string{"2.2.0", "2.1.0", "2.0.0", "1.0.0"}
        for _, v := range makerVersions {
            m := map[string]string{
                "@makerdao/dss":        v,
                "@makerdao/multicall":  v,
                "@makerdao/median":     v,
                "@makerdao/spot":       v,
                "@makerdao/jug":        v,
                "@makerdao/vat":        v,
                "@makerdao/cat":        v,
                "@makerdao/flip":       v,
                "@makerdao/flop":       v,
                "@makerdao/flap":       v,
                "@makerdao/end":        v,
                "@makerdao/esm":        v,
                "@makerdao/pause":      v,
                "@makerdao/dog":        v,
                "@makerdao/clip":       v,
                "@makerdao/calc":       v,
                "@makerdao/abaci":      v,
                "@makerdao/init":       v,
                "@makerdao/join":       v,
                "@makerdao/exit":       v,
                "@makerdao/fork":       v,
                "@makerdao/ilk":        v,
                "@makerdao/urn":        v,
                "@makerdao/guy":        v,
                "@makerdao/box":        v,
                "@makerdao/box2":       v,
                "@makerdao/box3":       v,
                "@makerdao/box4":       v,
                "@makerdao/box5":       v,
                "@makerdao/box6":       v,
                "@makerdao/box7":       v,
                "@makerdao/box8":       v,
                "@makerdao/box9":       v,
                "@makerdao/box10":      v,
                "@makerdao/box11":      v,
                "@makerdao/box12":      v,
                "@makerdao/box13":      v,
                "@makerdao/box14":      v,
                "@makerdao/box15":      v,
                "@makerdao/box16":      v,
                "@makerdao/box17":      v,
                "@makerdao/box18":      v,
                "@makerdao/box19":      v,
                "@makerdao/box20":      v,
                "@makerdao/box21":      v,
                "@makerdao/box22":      v,
                "@makerdao/box23":      v,
                "@makerdao/box24":      v,
                "@makerdao/box25":      v,
                "@makerdao/box26":      v,
                "@makerdao/box27":      v,
                "@makerdao/box28":      v,
                "@makerdao/box29":      v,
                "@makerdao/box30":      v,
                "@makerdao/box31":      v,
                "@makerdao/box32":      v,
                "@makerdao/box33":      v,
                "@makerdao/box34":      v,
                "@makerdao/box35":      v,
                "@makerdao/box36":      v,
                "@makerdao/box37":      v,
                "@makerdao/box38":      v,
                "@makerdao/box39":      v,
                "@makerdao/box40":      v,
                "@makerdao/box41":      v,
                "@makerdao/box42":      v,
                "@makerdao/box43":      v,
                "@makerdao/box44":      v,
                "@makerdao/box45":      v,
                "@makerdao/box46":      v,
                "@makerdao/box47":      v,
                "@makerdao/box48":      v,
                "@makerdao/box49":      v,
                "@makerdao/box50":      v,
            }
            attempts = append(attempts, m)
        }
    }
    
    // SushiSwap packages
    var hasSushi bool
    for _, d := range detected {
        if strings.HasPrefix(d, "@sushiswap/") || strings.Contains(d, "sushi") {
            hasSushi = true
            break
        }
    }
    
    if hasSushi {
        sushiVersions := []string{"1.0.0", "0.9.0", "0.8.0"}
        for _, v := range sushiVersions {
            m := map[string]string{
                "@sushiswap/core":      v,
                "@sushiswap/periphery": v,
                "@sushiswap/trident":   v,
                "@sushiswap/bentobox":  v,
                "sushiswap":            v,
            }
            attempts = append(attempts, m)
        }
    }
    
    // Curve packages
    var hasCurve bool
    for _, d := range detected {
        if strings.HasPrefix(d, "@curvefi/") || strings.Contains(d, "curve") {
            hasCurve = true
            break
        }
    }
    
    if hasCurve {
        curveVersions := []string{"2.0.0", "1.0.0"}
        for _, v := range curveVersions {
            m := map[string]string{
                "@curvefi/contracts": v,
                "@curvefi/vyper":     v,
                "curve-contracts":    v,
            }
            attempts = append(attempts, m)
        }
    }
    
    // Yearn packages
    var hasYearn bool
    for _, d := range detected {
        if strings.HasPrefix(d, "@yearn/") || strings.Contains(d, "yearn") {
            hasYearn = true
            break
        }
    }
    
    if hasYearn {
        yearnVersions := []string{"0.4.0", "0.3.0", "0.2.0", "0.1.0"}
        for _, v := range yearnVersions {
            m := map[string]string{
                "@yearn/contracts": v,
                "@yearn/vaults":    v,
                "@yearn/strategies": v,
                "yearn-vaults":     v,
            }
            attempts = append(attempts, m)
        }
    }
    
    // Always include an empty attempt last just in case
    attempts = append(attempts, nil)
    return attempts
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
// versionPins maps package name => version to pin, e.g. "@openzeppelin/contracts" => "4.8.3".
func collectSources(primaryName string, primaryContent string, versionPins map[string]string) (map[string]string, error) {
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
            data, err := fetchImport(resolved, versionPins)
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

// fetchImport retrieves Solidity source code by virtual key.
// Supported keys:
// - Absolute package paths (e.g., "@openzeppelin/contracts/.../ERC20.sol"): fetched via multiple CDNs.
// - HTTP(S) URLs: fetched directly.
// Other forms are currently unsupported and will return an error.
func fetchImport(key string, versionPins map[string]string) (string, error) {
    // direct URL import
    if strings.HasPrefix(key, "http://") || strings.HasPrefix(key, "https://") {
        return httpGetText(key)
    }

    // NPM-style package import, route via multiple CDNs with fallback
    if strings.HasPrefix(key, "@") || strings.Contains(key, "/") {
        // split package and rest
        pkg, rest := extractPackageAndRest(key)
        pinned := ""
        if versionPins != nil {
            if v, ok := versionPins[pkg]; ok && v != "" {
                pinned = "@" + v
            }
        }
        
        // build URL: https://unpkg.com/<pkg>@<version>/<rest>
        pathPart := pkg + pinned
        if rest != "" {
            pathPart += "/" + rest
        }
        
        // Try multiple CDNs with fallback
        cdnURLs := []string{
            "https://unpkg.com/" + strings.TrimPrefix(pathPart, "/"),
            "https://cdn.jsdelivr.net/npm/" + strings.TrimPrefix(pathPart, "/"),
            "https://unpkg.com/" + strings.TrimPrefix(pathPart, "/") + "?meta",
        }
        
        var lastErr error
        for _, url := range cdnURLs {
            data, err := httpGetText(url)
            if err == nil {
                return data, nil
            }
            lastErr = err
        }
        
        // If all CDNs failed, try without version pinning as last resort
        if pinned != "" {
            fallbackPath := pkg
            if rest != "" {
                fallbackPath += "/" + rest
            }
            
            fallbackURLs := []string{
                "https://unpkg.com/" + strings.TrimPrefix(fallbackPath, "/"),
                "https://cdn.jsdelivr.net/npm/" + strings.TrimPrefix(fallbackPath, "/"),
            }
            
            for _, url := range fallbackURLs {
                data, err := httpGetText(url)
                if err == nil {
                    return data, nil
                }
            }
        }
        
        return "", fmt.Errorf("failed to fetch from all CDNs: %w", lastErr)
    }

    return "", fmt.Errorf("unsupported import path: %s", key)
}

// httpGetText performs a simple GET request and returns the body as string if status is 200.
func httpGetText(url string) (string, error) {
    // use a short-lived client with timeout
    client := &http.Client{
        Timeout: 30 * time.Second, // Increased timeout for CDN requests
    }
    
    req, err := http.NewRequest("GET", url, nil)
    if err != nil {
        return "", err
    }
    
    // set basic headers to improve CDN compatibility
    req.Header.Set("User-Agent", "ncogearthchain-api-graphql/solidity-import-resolver")
    req.Header.Set("Accept", "text/plain,text/solidity,*/*")
    req.Header.Set("Accept-Encoding", "gzip, deflate")
    req.Header.Set("Cache-Control", "no-cache")
    
    // Add retry logic
    maxRetries := 3
    var lastErr error
    
    for attempt := 0; attempt < maxRetries; attempt++ {
        if attempt > 0 {
            // Exponential backoff
            time.Sleep(time.Duration(attempt) * time.Second)
        }
        
        resp, err := client.Do(req)
        if err != nil {
            lastErr = err
            continue
        }
        
        defer resp.Body.Close()
        
        if resp.StatusCode == http.StatusOK {
            // Check if response is gzipped
            var reader io.Reader = resp.Body
            if resp.Header.Get("Content-Encoding") == "gzip" {
                gzReader, err := gzip.NewReader(resp.Body)
                if err != nil {
                    lastErr = fmt.Errorf("failed to create gzip reader: %w", err)
                    continue
                }
                defer gzReader.Close()
                reader = gzReader
            }
            
            data, err := io.ReadAll(io.LimitReader(reader, 1<<20)) // 1MB limit
            if err != nil {
                lastErr = fmt.Errorf("failed to read response body: %w", err)
                continue
            }
            
            return string(data), nil
        }
        
        // If it's a 404, don't retry
        if resp.StatusCode == http.StatusNotFound {
            return "", fmt.Errorf("HTTP 404 from %s: file not found", url)
        }
        
        // For other status codes, try to read error body
        body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
        lastErr = fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, url, string(body))
        
        // Don't retry on client errors (4xx)
        if resp.StatusCode >= 400 && resp.StatusCode < 500 {
            break
        }
    }
    
    return "", fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

// compileSolidityStandardJSON compiles a single-source Solidity input using solc --standard-json
// and respects optimizer settings. Optional evmVersion and viaIR can be provided to match build.
// versionPins maps package name => version string (e.g., "@openzeppelin/contracts" => "4.8.3").
func compileSolidityStandardJSON(solcPath string, source string, optimized bool, runs int32, evmVersion string, viaIR bool, versionPins map[string]string) (map[string]compiledArtifact, error) {
    // resolve imports and collect all source units
    sourcesCollected, err := collectSources("input.sol", source, versionPins)
    if err != nil {
        return nil, err
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
            Remappings      []string                        `json:"remappings,omitempty"`
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
			"*": {"abi", "evm.bytecode.object", "evm.deployedBytecode.object"},
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
                    Object         string                                   `json:"object"`
                    LinkReferences map[string]map[string][]linkPos          `json:"linkReferences"`
                } `json:"bytecode"`
                DeployedBytecode struct {
                    Object         string                                   `json:"object"`
                    LinkReferences map[string]map[string][]linkPos          `json:"linkReferences"`
                } `json:"deployedBytecode"`
            } `json:"evm"`
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
				Name:            name,
				Code:            hexWithPrefix(c.Evm.Bytecode.Object),
				RuntimeCode:     hexWithPrefix(c.Evm.DeployedBytecode.Object),
				Abi:             c.Abi,
				CompilerVersion: stdOut.Version,
                CreationLinkRefs: flattenLinkReferences(c.Evm.Bytecode.LinkReferences),
                RuntimeLinkRefs:  flattenLinkReferences(c.Evm.DeployedBytecode.LinkReferences),
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

	// try to compile the source code provided with explicit optimizer settings
    // Attempt auto version pinning for popular packages if needed.
    // First pass: try without pins.
    compiled, err := compileSolidityStandardJSON(compilerPath, sc.SourceCode, sc.IsOptimized, sc.OptimizeRuns, sc.EvmVersion, sc.ViaIR, nil)
	p.log.Debugf("compiled output: %+v", compiled)
	if err != nil {
        p.log.Errorf("solidity code compilation failed with compiler %s: %s", compilerPath, err.Error())
        // Auto-detect likely packages and try a set of historical pins (OpenZeppelin).
        detected := detectPackages(sc.SourceCode)
        if len(detected) > 0 {
            pinsList := generateVersionPinAttempts(detected)
            for _, pins := range pinsList {
                compiled, err = compileSolidityStandardJSON(compilerPath, sc.SourceCode, sc.IsOptimized, sc.OptimizeRuns, sc.EvmVersion, sc.ViaIR, pins)
                if err == nil {
                    p.log.Debugf("compiled with pins: %+v", pins)
                    break
                }
            }
            if err != nil {
                return err
            }
        } else {
            return err
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
			if 0 == len(sc.Name) {
				sc.Name = strings.TrimPrefix(name, "<stdin>:")
			}

			// update the contract data
			// update details
			sc.Compiler = fmt.Sprintf("Solidity %s", detail.CompilerVersion)
			if len(detail.Abi) > 0 {
				sc.Abi = string(detail.Abi)
			}

			// set validated time stamp (now)
			now := hexutil.Uint64(uint64(time.Now().Unix()))
			sc.Validated = &now

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

		fmt.Printf("on-chain code: %s\n", hexutil.Encode(onChainCode))
		fmt.Printf("compiled output: %+v\n", compiled)

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
            compiledRuntime = maskBytesAtPositions(compiledRuntime, detail.RuntimeLinkRefs)

			// compare equality with on-chain code prefix-equality or exact? Use exact length match
			if len(onChainCode) >= len(compiledRuntime) {
                // mask the same positions on on-chain code slice
                maskedOnChain := maskBytesAtPositions(onChainCode[:len(compiledRuntime)], detail.RuntimeLinkRefs)
                if bytes.Equal(compiledRuntime, maskedOnChain) {
					// matched by runtime
					if 0 == len(sc.Name) {
						sc.Name = trimmedName
					}
					// update details
					sc.Compiler = fmt.Sprintf("Solidity %s", detail.CompilerVersion)
					if len(detail.Abi) > 0 {
						sc.Abi = string(detail.Abi)
					}

					// set validated time stamp (now)
					now := hexutil.Uint64(uint64(time.Now().Unix()))
					sc.Validated = &now

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
