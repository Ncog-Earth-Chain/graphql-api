// Package resolvers implements GraphQL resolvers to incoming API requests.
package resolvers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"html"
	"ncogearthchain-api-graphql/internal/logger"
	"ncogearthchain-api-graphql/internal/repository"
	"ncogearthchain-api-graphql/internal/types"
	"regexp"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// log represents the logger to be used by the contract resolver.
var log logger.Logger

// SetLogger sets the contract resolver logger to be used for logging.
func SetLogger(l logger.Logger) {
	log = l
}

const (
	// scValidationMinSourceCodeLength is the minimum length of a validated
	// smart contract source code.
	scMinSourceCodeLength = 32

	// scMaxNameLength is the maximum accepted length of a smart contract name.
	scMaxNameLength = 64

	// scMaxNameLength is the maximum accepted length of a smart contract version.
	scMaxVersionLength = 14

	// scMaxSupportLinkLength is the maximum accepted length of smart contract
	// support link.
	scMaxSupportLinkLength = 64
)

// scVersionSyntaxRegexp represents a regular expression for testing smart contract
// version string syntax. We enforce specific syntax on provided contract versions.
var scVersionSyntaxRegexp = regexp.MustCompile("^\\w?(\\d+\\.)+\\d+$")

// Contract represents resolvable blockchain smart contract structure.
type Contract struct {
	types.Contract
	// Enhanced verification fields
	VerificationMethod    string                 `json:"verificationMethod"`
	VerificationMetadata  map[string]interface{} `json:"verificationMetadata"`
	PackageDependencies   []string               `json:"packageDependencies"`
	VersionCompatibility  map[string]interface{} `json:"versionCompatibility"`
	CDNStatus            map[string]interface{} `json:"cdnStatus"`
	RegistryStats        map[string]interface{} `json:"registryStats"`
}

// ContractValidationInput represents an input structure used
// to validate contract source code against deployed contract byte code.
type ContractValidationInput struct {
	// Address represents the deployment address of the contract being validated.
	Address common.Address `json:"address"`

	// Name represents an optional name of the contract.
	Name *string `json:"name,omitempty"`

	// Version represents an optional version of the contract.
	// We assume version to be constructed from numbers and dots
	// with optional character at the beginning.
	// I.e. "v1.5.17"
	Version *string `json:"version,omitempty"`

	// SupportContact represents an optional contact information
	// the contract validator wants to publish with the contract
	// details.
	SupportContact *string `json:"supportContact,omitempty"`

	// License represents an optional contact open source license
	// being used.
	License *string `json:"license,omitempty"`

	// IsOptimized signals that the contract byte code was optimized
	// during compilation.
	Optimized bool `json:"optimized"`

	// OptimizeRuns represents number of optimization runs used
	// during the contract compilation.
	OptimizeRuns int32 `json:"optimizeRuns"`

	// CompilerVersion represents the Solidity compiler version to use
	// for validation. If empty, the default compiler will be used.
	CompilerVersion *string `json:"compilerVersion,omitempty"`

    // Optional EVM version used during compilation (e.g., london, paris, shanghai).
    EvmVersion *string `json:"evmVersion,omitempty"`

    // Optional flag to indicate compilation via IR pipeline.
    ViaIR bool `json:"viaIR"`

	// SourceCode represents the Solidity source code to be validated.
	SourceCode string `json:"sourceCode"`
}

// NewContract builds new resolvable smart contract structure.
func NewContract(con *types.Contract) *Contract {
	return &Contract{Contract: *con}
}

// DeployedBy resolves the deployment transaction of the contract.
func (con *Contract) DeployedBy() (*Transaction, error) {
	tr, err := repository.R().Transaction(&con.TransactionHash)
	return NewTransaction(tr), err
}

// sanitizeStringOption sanitizes and validates optional string value from the
// smart contract validation check.
func sanitizeStringOption(o *string, length int) (bool, *string) {
	// nil is always ok
	if o == nil {
		return true, nil
	}

	// escape the string
	val := html.EscapeString(*o)
	if len(val) > length {
		return false, nil
	}

	return true, &val
}

// isValidationValid checks the contract validation input and asses
// if it can be processed.
func isValidationValid(in *ContractValidationInput) error {
	// source code must be at least defined number of glyphs long
	if len(in.SourceCode) < scMinSourceCodeLength {
		return fmt.Errorf("contract source code is too short to be valid")
	}

	// collect sanitize result
	var res bool

	// check the name of the contract
	if res, in.Name = sanitizeStringOption(in.Name, scMaxNameLength); !res {
		return fmt.Errorf("contract name is too long to be valid")
	}

	// check the version of the contract
	if res, in.Version = sanitizeStringOption(in.Version, scMaxVersionLength); !res {
		return fmt.Errorf("contract version is too long to be valid")
	}

	// check the contact information of the contract
	if res, in.SupportContact = sanitizeStringOption(in.SupportContact, scMaxSupportLinkLength); !res {
		return fmt.Errorf("contract contact information is too long to be valid")
	}

	// validate the version syntax
	if in.Version != nil && !scVersionSyntaxRegexp.MatchString(*in.Version) {
		return fmt.Errorf("invalid version information provided")
	}

	// validate the version syntax
	if in.OptimizeRuns < 0 {
		return fmt.Errorf("invalid number of optimization runs provided")
	}

	return nil
}

// sourceHash calculates hash of the given source code so we can verify that
// incoming validation source code is not the same one we already know.
func sourceHash(sc string) common.Hash {
	// calculate SHA256 hash of the source code
	sum := sha256.Sum256([]byte(sc))
	return common.BytesToHash(sum[:])
}

// updateContractFromInput update Contract data from provided input structure.
func updateContractFromInput(con *ContractValidationInput, sc *types.Contract) {
	// update the contract detail and pass it to validation
	sc.SourceCode = con.SourceCode
	sc.IsOptimized = con.Optimized
	sc.OptimizeRuns = con.OptimizeRuns

	// pass the intended name
	if con.Name != nil {
		sc.Name = *con.Name
	}

	// pass the intended version
	if con.Version != nil {
		sc.Version = *con.Version
	}

	// pass the intended license
	if con.License != nil {
		sc.License = *con.License
	}

	// pass the intended support contact
	if con.SupportContact != nil {
		sc.SupportContact = *con.SupportContact
	}

	// pass the intended compiler version
	if con.CompilerVersion != nil {
		sc.CompilerVersion = *con.CompilerVersion
	}

    // pass EVM version and viaIR
    if con.EvmVersion != nil {
        sc.EvmVersion = *con.EvmVersion
    }
    sc.ViaIR = con.ViaIR
}

// ValidateContract resolves smart contract source code vs. deployed byte code and marks
// the contract as validated if the match is found. This function now uses the enhanced
// verification system with automatic dependency resolution and version management.
func (rs *rootResolver) ValidateContract(args *struct{ Contract ContractValidationInput }) (*Contract, error) {
	// validate the input
	if err := isValidationValid(&args.Contract); err != nil {
		log.Errorf("can not validate contract, validation request is not valid; %s", err.Error())
		return nil, err
	}

	// get a contract to be validated if any
	sc, err := repository.R().Contract(&args.Contract.Address)
	if err != nil {
		log.Errorf("contract [%s] not found", args.Contract.Address.String())
		return nil, err
	}

	// if we already have this source code, no need to do any updates
	hash := sourceHash(args.Contract.SourceCode)
	if sc.SourceCodeHash != nil && hash.String() == sc.SourceCodeHash.String() {
		log.Debugf("contract [%s] source code is already known", sc.Address.String())
		return NewContract(sc), nil
	}

	// copy relevant information from input into the contract struct
	sc.SourceCodeHash = &hash
	updateContractFromInput(&args.Contract, sc)

	// Try enhanced verification first, fallback to standard if not available
	if err := rs.validateContractEnhanced(sc); err != nil {
		log.Warnf("enhanced validation failed, falling back to standard validation: %s", err.Error())
		
		// Fallback to standard validation
		if err := repository.R().ValidateContract(sc); err != nil {
			log.Errorf("contract validation failed; %s", err.Error())
			return nil, err
		}
	} else {
		log.Infof("contract [%s] successfully validated using enhanced verification system", sc.Address.String())
	}

	// initiate contract syncing in a separated routine
	// we don't really need to wait for it, so let it run
	go rs.syncContract(*sc)

	// return the final updated contract
	return NewContract(sc), nil
}

// validateContractEnhanced attempts to validate the contract using the enhanced verification system.
// If the enhanced system is not available, it returns an error to trigger fallback.
func (rs *rootResolver) validateContractEnhanced(sc *types.Contract) error {
	// Check if enhanced verifier is available
	enhancedVerifier := repository.GetEnhancedContractVerifier()
	if enhancedVerifier == nil {
		return fmt.Errorf("enhanced verification system not available")
	}

	// Perform enhanced verification
	ctx := context.Background()
	if err := enhancedVerifier.VerifyContract(ctx, sc); err != nil {
		return fmt.Errorf("enhanced verification failed: %w", err)
	}

	// Enhanced verification successful
	return nil
}

// Enhanced verification system resolvers

// ContractVerificationInfo provides detailed verification information for a contract.
func (rs *rootResolver) ContractVerificationInfo(args *struct{ Address common.Address }) (*Contract, error) {
	// Get contract
	sc, err := repository.R().Contract(&args.Address)
	if err != nil {
		return nil, err
	}

	contract := NewContract(sc)
	
	// Populate enhanced verification fields if available
	rs.populateEnhancedContractFields(contract, sc.SourceCode)

	return contract, nil
}

// PackageCompatibilityMatrix provides compatibility information for detected packages.
func (rs *rootResolver) PackageCompatibilityMatrix(args *struct{ SourceCode string }) (map[string]interface{}, error) {
	enhancedVerifier := repository.GetEnhancedContractVerifier()
	if enhancedVerifier == nil {
		return map[string]interface{}{
			"error": "Enhanced verification system not available",
			"status": "disabled",
		}, nil
	}

	return enhancedVerifier.GetCompatibilityMatrix(args.SourceCode), nil
}

// OptimalPackageVersions suggests optimal package versions for the given source code.
func (rs *rootResolver) OptimalPackageVersions(args *struct{ SourceCode string }) (map[string]interface{}, error) {
	enhancedVerifier := repository.GetEnhancedContractVerifier()
	if enhancedVerifier == nil {
		return map[string]interface{}{
			"error": "Enhanced verification system not available",
			"status": "disabled",
		}, nil
	}

	suggestions := enhancedVerifier.SuggestOptimalVersions(args.SourceCode)
	return map[string]interface{}{
		"suggestions": suggestions,
		"status": "active",
	}, nil
}

// RegistryStatus provides current status of the enhanced verification registry system.
func (rs *rootResolver) RegistryStatus() (map[string]interface{}, error) {
	enhancedVerifier := repository.GetEnhancedContractVerifier()
	if enhancedVerifier == nil {
		return map[string]interface{}{
			"error": "Enhanced verification system not available",
			"status": "disabled",
		}, nil
	}

	return enhancedVerifier.GetRegistryStats(), nil
}

// CDNStatus provides current status of CDN endpoints for dependency resolution.
func (rs *rootResolver) CDNStatus() (map[string]interface{}, error) {
	enhancedVerifier := repository.GetEnhancedContractVerifier()
	if enhancedVerifier == nil {
		return map[string]interface{}{
			"error": "Enhanced verification system not available",
			"status": "disabled",
		}, nil
	}

	cdnStatus := enhancedVerifier.GetCDNStatus()
	return map[string]interface{}{
		"cdnStatus": cdnStatus,
		"status": "active",
	}, nil
}

// RefreshPackageCache refreshes the package registry cache.
func (rs *rootResolver) RefreshPackageCache() (bool, error) {
	enhancedVerifier := repository.GetEnhancedContractVerifier()
	if enhancedVerifier == nil {
		return false, fmt.Errorf("enhanced verification system not available")
	}

	enhancedVerifier.RefreshPackageCache()
	return true, nil
}

// PreValidateSourceCode checks source code for potential issues before deployment.
func (rs *rootResolver) PreValidateSourceCode(args *struct{ SourceCode string }) (map[string]interface{}, error) {
	enhancedVerifier := repository.GetEnhancedContractVerifier()
	if enhancedVerifier == nil {
		return map[string]interface{}{
			"error": "Enhanced verification system not available",
			"status": "disabled",
		}, nil
	}

	// Validate source code
	issues := enhancedVerifier.ValidateSourceCode(args.SourceCode)
	
	// Get package information
	packageInfo := enhancedVerifier.GetPackageInfo(args.SourceCode)
	
	// Get compatibility matrix
	compatibilityMatrix := enhancedVerifier.GetCompatibilityMatrix(args.SourceCode)
	
	// Get optimal versions
	optimalVersions := enhancedVerifier.SuggestOptimalVersions(args.SourceCode)

	return map[string]interface{}{
		"validationIssues": issues,
		"packageInfo": packageInfo,
		"compatibilityMatrix": compatibilityMatrix,
		"optimalVersions": optimalVersions,
		"status": "completed",
	}, nil
}

// Helper method to populate enhanced contract fields
func (rs *rootResolver) populateEnhancedContractFields(contract *Contract, sourceCode string) {
	enhancedVerifier := repository.GetEnhancedContractVerifier()
	if enhancedVerifier == nil {
		// Set default values when enhanced system is not available
		contract.VerificationMethod = "standard"
		contract.VerificationMetadata = map[string]interface{}{
			"verificationMethod": "standard",
			"enhancedSystemAvailable": false,
		}
		return
	}

	// Enhanced system is available
	contract.VerificationMethod = "enhanced_registry"
	
	// Extract package dependencies
	contract.PackageDependencies = rs.extractPackageNames(sourceCode)
	
	// Get version compatibility matrix
	contract.VersionCompatibility = enhancedVerifier.GetCompatibilityMatrix(sourceCode)
	
	// Get CDN status
	contract.CDNStatus = enhancedVerifier.GetCDNStatus()
	
	// Get registry stats
	contract.RegistryStats = enhancedVerifier.GetRegistryStats()
	
	// Build verification metadata
	contract.VerificationMetadata = map[string]interface{}{
		"verificationMethod": "enhanced_registry",
		"enhancedSystemAvailable": true,
		"totalPackages": len(contract.PackageDependencies),
		"cdnStatus": contract.CDNStatus,
		"registryStatus": "active",
	}
}

// extractPackageNames extracts package names from import statements in source code.
func (rs *rootResolver) extractPackageNames(sourceCode string) []string {
	// Extract package names from import statements
	// This is a simplified version - the actual implementation would use regex
	packages := []string{}
	
	// Look for common package patterns
	importPatterns := []string{
		"@openzeppelin/contracts",
		"@chainlink/contracts",
		"@uniswap/",
		"openzeppelin-solidity",
	}
	
	for _, pattern := range importPatterns {
		if strings.Contains(sourceCode, pattern) {
			packages = append(packages, pattern)
		}
	}
	
	return packages
}
