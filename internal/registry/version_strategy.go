package registry

import (
	"regexp"
	"strings"
)

// VersionStrategy manages version selection and compatibility strategies
type VersionStrategy struct {
	registry *PackageRegistry
}

// NewVersionStrategy creates a new version strategy instance
func NewVersionStrategy(registry *PackageRegistry) *VersionStrategy {
	return &VersionStrategy{
		registry: registry,
	}
}

// GenerateVersionAttempts generates a prioritized list of version combinations to try
func (vs *VersionStrategy) GenerateVersionAttempts(detectedPackages []string) []map[string]string {
	var attempts []map[string]string

	// Strategy 1: Try latest versions of all packages
	latestVersions := vs.getLatestVersions(detectedPackages)
	if len(latestVersions) > 0 {
		attempts = append(attempts, latestVersions)
	}

	// Strategy 2: Try stable version combinations (LTS versions)
	stableVersions := vs.getStableVersions(detectedPackages)
	if len(stableVersions) > 0 {
		attempts = append(attempts, stableVersions)
	}

	// Strategy 3: Try historical version combinations
	historicalVersions := vs.getHistoricalVersions(detectedPackages)
	attempts = append(attempts, historicalVersions...)

	// Strategy 4: Try fallback versions for known packages
	fallbackVersions := vs.getFallbackVersions(detectedPackages)
	attempts = append(attempts, fallbackVersions...)

	return attempts
}

// getLatestVersions gets the latest version for each detected package
func (vs *VersionStrategy) getLatestVersions(packages []string) map[string]string {
	versions := make(map[string]string)
	
	for _, pkg := range packages {
		if latest, err := vs.registry.GetLatestVersion(pkg); err == nil {
			versions[pkg] = latest
		}
	}
	
	return versions
}

// getStableVersions gets stable/LTS versions for each detected package
func (vs *VersionStrategy) getStableVersions(packages []string) map[string]string {
	versions := make(map[string]string)
	
	for _, pkg := range packages {
		if stable := vs.getStableVersion(pkg); stable != "" {
			versions[pkg] = stable
		}
	}
	
	return versions
}

// getStableVersion returns a stable version for a specific package
func (vs *VersionStrategy) getStableVersion(packageName string) string {
	// Define stable versions for popular packages
	stableVersions := map[string]string{
		"@openzeppelin/contracts": "4.9.6",    // Last v4.x stable
		"@chainlink/contracts":    "0.0.16",   // Latest stable
		"@uniswap/v3-core":       "1.0.1",    // Stable v3
		"@aave/core-v3":          "1.19.1",   // Latest stable
		"@compound-finance/compound-protocol": "3.1.0", // Latest stable
	}
	
	if stable, exists := stableVersions[packageName]; exists {
		return stable
	}
	
	// Try to find a stable version from registry
	if info, err := vs.registry.DiscoverPackage(packageName); err == nil {
		for _, version := range info.Versions {
			if vs.isStableVersion(version) {
				return version
			}
		}
	}
	
	return ""
}

// isStableVersion checks if a version is considered stable
func (vs *VersionStrategy) isStableVersion(version string) bool {
	// Consider versions stable if they don't contain pre-release indicators
	return !strings.Contains(version, "-") && 
		   !strings.Contains(version, "alpha") && 
		   !strings.Contains(version, "beta") && 
		   !strings.Contains(version, "rc") &&
		   !strings.Contains(version, "dev")
}

// getHistoricalVersions generates historical version combinations
func (vs *VersionStrategy) getHistoricalVersions(packages []string) []map[string]string {
	var attempts []map[string]string
	
	// For each package, try a few historical versions
	for _, pkg := range packages {
		if info, err := vs.registry.DiscoverPackage(pkg); err == nil {
			// Try up to 5 historical versions
			maxVersions := 5
			if len(info.Versions) < maxVersions {
				maxVersions = len(info.Versions)
			}
			
			for i := 0; i < maxVersions; i++ {
				attempt := map[string]string{pkg: info.Versions[i]}
				attempts = append(attempts, attempt)
			}
		}
	}
	
	return attempts
}

// getFallbackVersions provides fallback versions for known packages
func (vs *VersionStrategy) getFallbackVersions(packages []string) []map[string]string {
	var attempts []map[string]string
	
	// Define fallback versions for common packages
	fallbackVersions := map[string][]string{
		"@openzeppelin/contracts": {"5.0.1", "4.9.6", "3.4.2", "2.5.1"},
		"@chainlink/contracts":    {"0.0.16", "0.0.15", "0.0.14"},
		"@uniswap/v3-core":       {"1.0.1", "1.0.0"},
		"@uniswap/v2-core":       {"1.0.1", "1.0.0"},
		"@aave/core-v3":          {"1.19.1", "1.18.0", "1.17.0"},
		"@compound-finance/compound-protocol": {"3.1.0", "3.0.0", "2.8.0"},
		"@synthetixio/contracts": {"2.86.0", "2.85.0", "2.84.0"},
		"@balancer-labs/v2-vault": {"0.4.0", "0.3.0", "0.2.0"},
		"@gnosis/safe-contracts": {"1.4.0", "1.3.0", "1.2.0"},
		"@makerdao/dss":          {"2.2.0", "2.1.0", "2.0.0"},
		"@sushiswap/core":        {"1.0.0", "0.8.0"},
		"@curvefi/contracts":     {"2.0.0", "1.0.0"},
		"@yearn/contracts":       {"0.4.0", "0.3.0", "0.2.0"},
	}
	
	for _, pkg := range packages {
		if fallbacks, exists := fallbackVersions[pkg]; exists {
			for _, version := range fallbacks {
				attempt := map[string]string{pkg: version}
				attempts = append(attempts, attempt)
			}
		}
	}
	
	return attempts
}

// DetectPackagesFromSource detects package names from Solidity source code
func (vs *VersionStrategy) DetectPackagesFromSource(source string) []string {
	var packages []string
	
	// Extract imports
	imports := vs.extractImports(source)
	
	// Extract package names from imports
	for _, importPath := range imports {
		if pkg := vs.extractPackageName(importPath); pkg != "" {
			packages = append(packages, pkg)
		}
	}
	
	// Also check comments for package references
	commentPackages := vs.extractPackagesFromComments(source)
	packages = append(packages, commentPackages...)
	
	// Remove duplicates
	return vs.removeDuplicates(packages)
}

// extractImports extracts import statements from Solidity source
func (vs *VersionStrategy) extractImports(source string) []string {
	var imports []string
	
	// Regex to match import statements
	importRegex := regexp.MustCompile(`import\s+["']([^"']+)["']\s*;?`)
	matches := importRegex.FindAllStringSubmatch(source, -1)
	
	for _, match := range matches {
		if len(match) > 1 {
			imports = append(imports, match[1])
		}
	}
	
	return imports
}

// extractPackagesFromComments extracts package references from comments
func (vs *VersionStrategy) extractPackagesFromComments(source string) []string {
	var packages []string
	
	// Common package patterns in comments
	patterns := []string{
		`@openzeppelin/contracts`,
		`@chainlink/contracts`,
		`@uniswap/`,
		`@aave/`,
		`@compound-finance/`,
		`@synthetixio/`,
		`@balancer-labs/`,
		`@gnosis/`,
		`@makerdao/`,
		`@sushiswap/`,
		`@curvefi/`,
		`@yearn/`,
		`openzeppelin-solidity`,
		`chainlink`,
		`uniswap`,
		`aave`,
		`compound`,
		`synthetix`,
		`balancer`,
		`gnosis`,
		`makerdao`,
		`sushiswap`,
		`curve`,
		`yearn`,
	}
	
	for _, pattern := range patterns {
		if strings.Contains(source, pattern) {
			packages = append(packages, pattern)
		}
	}
	
	return packages
}

// extractPackageName extracts the package name from an import path
func (vs *VersionStrategy) extractPackageName(importPath string) string {
	// Handle scoped packages (@org/package)
	if strings.HasPrefix(importPath, "@") {
		parts := strings.Split(importPath, "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
	}
	
	// Handle regular packages
	parts := strings.Split(importPath, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	
	return ""
}

// removeDuplicates removes duplicate package names
func (vs *VersionStrategy) removeDuplicates(packages []string) []string {
	seen := make(map[string]bool)
	var result []string
	
	for _, pkg := range packages {
		if !seen[pkg] {
			seen[pkg] = true
			result = append(result, pkg)
		}
	}
	
	return result
}

// GetCompatibilityMatrix returns compatibility information between package versions
func (vs *VersionStrategy) GetCompatibilityMatrix(packages []string) map[string]map[string][]string {
	matrix := make(map[string]map[string][]string)
	
	for _, pkg := range packages {
		matrix[pkg] = make(map[string][]string)
		
		if info, err := vs.registry.DiscoverPackage(pkg); err == nil {
			// Group versions by major version
			majorVersions := make(map[string][]string)
			for _, version := range info.Versions {
				major := vs.getMajorVersion(version)
				majorVersions[major] = append(majorVersions[major], version)
			}
			
			// Store compatibility groups
			for major, versions := range majorVersions {
				matrix[pkg][major] = versions
			}
		}
	}
	
	return matrix
}

// getMajorVersion extracts the major version from a semantic version
func (vs *VersionStrategy) getMajorVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) > 0 {
		return parts[0]
	}
	return version
}

// SuggestOptimalVersions suggests optimal version combinations based on compatibility
func (vs *VersionStrategy) SuggestOptimalVersions(packages []string) map[string]string {
	suggestions := make(map[string]string)
	
	// Priority order for version selection
	priorityOrder := []string{
		"@openzeppelin/contracts",    // Most common, try latest stable
		"@chainlink/contracts",       // Try latest stable
		"@uniswap/v3-core",          // Try latest stable
		"@aave/core-v3",             // Try latest stable
		"@compound-finance/compound-protocol", // Try latest stable
	}
	
	// Sort packages by priority
	sortedPackages := vs.sortByPriority(packages, priorityOrder)
	
	for _, pkg := range sortedPackages {
		if optimal := vs.getOptimalVersion(pkg); optimal != "" {
			suggestions[pkg] = optimal
		}
	}
	
	return suggestions
}

// sortByPriority sorts packages by their priority order
func (vs *VersionStrategy) sortByPriority(packages []string, priorityOrder []string) []string {
	priorityMap := make(map[string]int)
	for i, pkg := range priorityOrder {
		priorityMap[pkg] = i
	}
	
	// Sort packages by priority (lower index = higher priority)
	sorted := make([]string, len(packages))
	copy(sorted, packages)
	
	// Simple bubble sort by priority
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			priority1 := priorityMap[sorted[j]]
			priority2 := priorityMap[sorted[j+1]]
			
			// If priority not found, put at end
			if priority1 == 0 && sorted[j] != priorityOrder[0] {
				priority1 = 999
			}
			if priority2 == 0 && sorted[j+1] != priorityOrder[0] {
				priority2 = 999
			}
			
			if priority1 > priority2 {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}
	
	return sorted
}

// getOptimalVersion returns the optimal version for a package
func (vs *VersionStrategy) getOptimalVersion(packageName string) string {
	// Try to get the latest stable version
	if stable := vs.getStableVersion(packageName); stable != "" {
		return stable
	}
	
	// Fallback to latest version
	if latest, err := vs.registry.GetLatestVersion(packageName); err == nil {
		return latest
	}
	
	return ""
}
