const chai = require('chai');
const expect = chai.expect;

// Mock data for testing the enhanced registry system
const mockContracts = {
    // OpenZeppelin v5.x contract
    openzeppelinV5: {
        name: "SimpleStorageV5",
        source: `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "@openzeppelin/contracts/access/Ownable.sol";

contract SimpleStorageV5 is Ownable {
    uint256 private _value;
    
    event ValueChanged(uint256 newValue);
    
    constructor() Ownable(msg.sender) {}
    
    function setValue(uint256 newValue) public onlyOwner {
        _value = newValue;
        emit ValueChanged(newValue);
    }
    
    function getValue() public view returns (uint256) {
        return _value;
    }
}`,
        expectedPackages: ["@openzeppelin/contracts"],
        expectedVersions: ["5.0.1", "5.0.0"]
    },

    // OpenZeppelin v4.x contract
    openzeppelinV4: {
        name: "SimpleStorageV4",
        source: `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import "@openzeppelin/contracts/access/Ownable.sol";

contract SimpleStorageV4 is Ownable {
    uint256 private _value;
    
    event ValueChanged(uint256 newValue);
    
    constructor() {
        _transferOwnership(_msgSender());
    }
    
    function setValue(uint256 newValue) public onlyOwner {
        _value = newValue;
        emit ValueChanged(newValue);
    }
    
    function getValue() public view returns (uint256) {
        return _value;
    }
}`,
        expectedPackages: ["@openzeppelin/contracts"],
        expectedVersions: ["4.9.6", "4.9.5", "4.9.4"]
    },

    // Mixed packages contract
    mixedPackages: {
        name: "DeFiContract",
        source: `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@chainlink/contracts/src/v0.8/interfaces/AggregatorV3Interface.sol";
import "@uniswap/v3-core/contracts/interfaces/IUniswapV3Pool.sol";
import "@aave/core-v3/contracts/interfaces/IPool.sol";

contract DeFiContract {
    ERC20 public token;
    AggregatorV3Interface public priceFeed;
    IUniswapV3Pool public pool;
    IPool public aavePool;
    
    constructor(
        address _token,
        address _priceFeed,
        address _pool,
        address _aavePool
    ) {
        token = ERC20(_token);
        priceFeed = AggregatorV3Interface(_priceFeed);
        pool = IUniswapV3Pool(_pool);
        aavePool = IPool(_aavePool);
    }
    
    function getTokenBalance() public view returns (uint256) {
        return token.balanceOf(address(this));
    }
    
    function getLatestPrice() public view returns (int) {
        (, int price,,,) = priceFeed.latestRoundData();
        return price;
    }
}`,
        expectedPackages: [
            "@openzeppelin/contracts",
            "@chainlink/contracts",
            "@uniswap/v3-core",
            "@aave/core-v3"
        ],
        expectedVersions: {
            "@openzeppelin/contracts": ["4.9.6", "4.9.5", "4.9.4"],
            "@chainlink/contracts": ["0.0.16", "0.0.15", "0.0.14"],
            "@uniswap/v3-core": ["1.0.1", "1.0.0"],
            "@aave/core-v3": ["1.19.1", "1.18.0", "1.17.0"]
        }
    },

    // Governance contract with Compound
    governanceContract: {
        name: "GovernanceToken",
        source: `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import "@compound-finance/compound-protocol/contracts/Governance/Comp.sol";

contract GovernanceToken is ERC20, Ownable {
    Comp public compToken;
    
    constructor(address _compToken) ERC20("Governance", "GOV") {
        compToken = Comp(_compToken);
        _transferOwnership(_msgSender());
    }
    
    function mint(address to, uint256 amount) public onlyOwner {
        _mint(to, amount);
    }
    
    function getCompBalance() public view returns (uint256) {
        return compToken.balanceOf(address(this));
    }
}`,
        expectedPackages: [
            "@openzeppelin/contracts",
            "@compound-finance/compound-protocol"
        ],
        expectedVersions: {
            "@openzeppelin/contracts": ["4.9.6", "4.9.5", "4.9.4"],
            "@compound-finance/compound-protocol": ["3.1.0", "3.0.0", "2.8.0"]
        }
    },

    // Legacy OpenZeppelin contract
    legacyOpenZeppelin: {
        name: "LegacyContract",
        source: `// SPDX-License-Identifier: MIT
pragma solidity ^0.7.6;

import "openzeppelin-solidity/contracts/token/ERC20/ERC20.sol";
import "openzeppelin-solidity/contracts/access/Ownable.sol";

contract LegacyContract is ERC20, Ownable {
    constructor() ERC20("Legacy", "LGC") {
        _transferOwnership(_msgSender());
    }
    
    function mint(address to, uint256 amount) public onlyOwner {
        _mint(to, amount);
    }
}`,
        expectedPackages: ["openzeppelin-solidity"],
        expectedVersions: ["2.5.1", "2.5.0", "2.4.0"]
    },

    // Custom package contract
    customPackage: {
        name: "CustomContract",
        source: `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import "@custom/package/contracts/MyToken.sol";
import "@another/package/contracts/Helper.sol";

contract CustomContract {
    MyToken public token;
    Helper public helper;
    
    constructor(address _token, address _helper) {
        token = MyToken(_token);
        helper = Helper(_helper);
    }
    
    function doSomething() public {
        helper.help();
    }
}`,
        expectedPackages: ["@custom/package", "@another/package"],
        expectedVersions: [] // Unknown packages, should fall back to basic discovery
    }
};

// Test suite for the enhanced registry system
describe('Enhanced Registry System Tests', function() {
    
    describe('Package Detection', function() {
        it('should detect OpenZeppelin v5.x packages correctly', function() {
            const contract = mockContracts.openzeppelinV5;
            const detectedPackages = detectPackages(contract.source);
            
            expect(detectedPackages).to.include('@openzeppelin/contracts');
            expect(detectedPackages).to.have.lengthOf(1);
        });

        it('should detect OpenZeppelin v4.x packages correctly', function() {
            const contract = mockContracts.openzeppelinV4;
            const detectedPackages = detectPackages(contract.source);
            
            expect(detectedPackages).to.include('@openzeppelin/contracts');
            expect(detectedPackages).to.have.lengthOf(1);
        });

        it('should detect multiple packages in mixed contracts', function() {
            const contract = mockContracts.mixedPackages;
            const detectedPackages = detectPackages(contract.source);
            
            expect(detectedPackages).to.include('@openzeppelin/contracts');
            expect(detectedPackages).to.include('@chainlink/contracts');
            expect(detectedPackages).to.include('@uniswap/v3-core');
            expect(detectedPackages).to.include('@aave/core-v3');
            expect(detectedPackages).to.have.lengthOf(4);
        });

        it('should detect legacy OpenZeppelin packages', function() {
            const contract = mockContracts.legacyOpenZeppelin;
            const detectedPackages = detectPackages(contract.source);
            
            expect(detectedPackages).to.include('openzeppelin-solidity');
            expect(detectedPackages).to.have.lengthOf(1);
        });

        it('should detect custom packages', function() {
            const contract = mockContracts.customPackage;
            const detectedPackages = detectPackages(contract.source);
            
            expect(detectedPackages).to.include('@custom/package');
            expect(detectedPackages).to.include('@another/package');
            expect(detectedPackages).to.have.lengthOf(2);
        });
    });

    describe('Version Strategy', function() {
        it('should generate version attempts for OpenZeppelin v5.x', function() {
            const packages = ['@openzeppelin/contracts'];
            const versionAttempts = generateVersionAttempts(packages);
            
            expect(versionAttempts).to.be.an('array');
            expect(versionAttempts).to.have.length.greaterThan(0);
            
            // Should try v5.x first
            const firstAttempt = versionAttempts[0];
            expect(firstAttempt['@openzeppelin/contracts']).to.match(/^5\./);
        });

        it('should generate version attempts for OpenZeppelin v4.x', function() {
            const packages = ['@openzeppelin/contracts'];
            const versionAttempts = generateVersionAttempts(packages);
            
            // Should include v4.x versions
            const hasV4 = versionAttempts.some(attempt => 
                attempt['@openzeppelin/contracts'].startsWith('4.')
            );
            expect(hasV4).to.be.true;
        });

        it('should generate version attempts for mixed packages', function() {
            const packages = ['@openzeppelin/contracts', '@chainlink/contracts'];
            const versionAttempts = generateVersionAttempts(packages);
            
            expect(versionAttempts).to.be.an('array');
            expect(versionAttempts).to.have.length.greaterThan(0);
            
            // Should include both packages
            const firstAttempt = versionAttempts[0];
            expect(firstAttempt).to.have.property('@openzeppelin/contracts');
            expect(firstAttempt).to.have.property('@chainlink/contracts');
        });

        it('should prioritize stable versions', function() {
            const packages = ['@openzeppelin/contracts'];
            const versionAttempts = generateVersionAttempts(packages);
            
            // First few attempts should be stable versions
            const stableVersions = ['5.0.1', '4.9.6', '3.4.2'];
            const firstAttempts = versionAttempts.slice(0, 3);
            
            firstAttempts.forEach((attempt, index) => {
                expect(attempt['@openzeppelin/contracts']).to.equal(stableVersions[index]);
            });
        });
    });

    describe('Dependency Resolution', function() {
        it('should resolve OpenZeppelin dependencies', function() {
            const importPath = '@openzeppelin/contracts/access/Ownable.sol';
            const versionPins = {'@openzeppelin/contracts': '4.9.6'};
            
            // Mock dependency resolution
            const resolved = resolveDependency(importPath, versionPins);
            expect(resolved).to.be.a('string');
            expect(resolved).to.contain('contract Ownable');
        });

        it('should handle missing dependencies gracefully', function() {
            const importPath = '@nonexistent/package/Contract.sol';
            const versionPins = {};
            
            // Should return error for non-existent packages
            expect(() => resolveDependency(importPath, versionPins)).to.throw();
        });

        it('should try multiple CDNs for dependency resolution', function() {
            const importPath = '@openzeppelin/contracts/access/Ownable.sol';
            const versionPins = {'@openzeppelin/contracts': '4.9.6'};
            
            // Mock CDN fallback
            const resolved = resolveDependencyWithFallback(importPath, versionPins);
            expect(resolved).to.be.a('string');
        });
    });

    describe('Contract Verification', function() {
        it('should verify OpenZeppelin v5.x contract successfully', function() {
            const contract = mockContracts.openzeppelinV5;
            const result = verifyContract(contract.source, 'mock_bytecode');
            
            expect(result.success).to.be.true;
            expect(result.versionPins).to.have.property('@openzeppelin/contracts');
            expect(result.versionPins['@openzeppelin/contracts']).to.match(/^5\./);
        });

        it('should verify OpenZeppelin v4.x contract successfully', function() {
            const contract = mockContracts.openzeppelinV4;
            const result = verifyContract(contract.source, 'mock_bytecode');
            
            expect(result.success).to.be.true;
            expect(result.versionPins).to.have.property('@openzeppelin/contracts');
            expect(result.versionPins['@openzeppelin/contracts']).to.match(/^4\./);
        });

        it('should verify mixed package contract successfully', function() {
            const contract = mockContracts.mixedPackages;
            const result = verifyContract(contract.source, 'mock_bytecode');
            
            expect(result.success).to.be.true;
            expect(result.versionPins).to.have.property('@openzeppelin/contracts');
            expect(result.versionPins).to.have.property('@chainlink/contracts');
            expect(result.versionPins).to.have.property('@uniswap/v3-core');
            expect(result.versionPins).to.have.property('@aave/core-v3');
        });

        it('should handle verification failures gracefully', function() {
            const invalidSource = 'invalid solidity code';
            const result = verifyContract(invalidSource, 'mock_bytecode');
            
            expect(result.success).to.be.false;
            expect(result.error).to.be.a('string');
            expect(result.attempts).to.be.greaterThan(0);
        });
    });

    describe('Registry Management', function() {
        it('should refresh package cache', function() {
            const result = refreshPackageCache();
            expect(result).to.be.true;
        });

        it('should get CDN status', function() {
            const cdnStatus = getCDNStatus();
            expect(cdnStatus).to.be.an('object');
            expect(cdnStatus).to.have.property('unpkg.com');
            expect(cdnStatus).to.have.property('cdn.jsdelivr.net');
        });

        it('should get registry statistics', function() {
            const stats = getRegistryStats();
            expect(stats).to.be.an('object');
            expect(stats).to.have.property('timestamp');
            expect(stats).to.have.property('cdnStatus');
        });
    });

    describe('Performance and Scalability', function() {
        it('should handle large dependency trees efficiently', function() {
            const largeContract = generateLargeContract(100); // 100 imports
            const startTime = Date.now();
            
            const result = verifyContract(largeContract.source, 'mock_bytecode');
            const endTime = Date.now();
            
            expect(result.success).to.be.true;
            expect(endTime - startTime).to.be.lessThan(5000); // Should complete within 5 seconds
        });

        it('should cache package information efficiently', function() {
            const packages = ['@openzeppelin/contracts'];
            
            // First call should populate cache
            const startTime1 = Date.now();
            const info1 = getPackageInfo(packages[0]);
            const endTime1 = Date.now();
            
            // Second call should use cache
            const startTime2 = Date.now();
            const info2 = getPackageInfo(packages[0]);
            const endTime2 = Date.now();
            
            expect(info1).to.deep.equal(info2);
            expect(endTime2 - startTime2).to.be.lessThan(endTime1 - startTime1);
        });
    });
});

// Mock functions for testing (these would be replaced with actual implementations)
function detectPackages(source) {
    const packages = new Set();
    
    // Extract imports
    const importRegex = /import\s+["']([^"']+)["']/g;
    let match;
    
    while ((match = importRegex.exec(source)) !== null) {
        const importPath = match[1];
        const packageName = extractPackageName(importPath);
        if (packageName) {
            packages.add(packageName);
        }
    }
    
    // Also check comments for package references
    const commentPackages = detectPackagesFromComments(source);
    commentPackages.forEach(pkg => packages.add(pkg));
    
    return Array.from(packages);
}

function extractPackageName(importPath) {
    if (importPath.startsWith('@')) {
        const parts = importPath.split('/');
        if (parts.length >= 2) {
            return parts[0] + '/' + parts[1];
        }
    } else {
        const parts = importPath.split('/');
        return parts[0];
    }
    return null;
}

function detectPackagesFromComments(source) {
    const packages = [];
    const patterns = [
        '@openzeppelin/contracts',
        '@chainlink/contracts',
        '@uniswap/',
        '@aave/',
        '@compound-finance/',
        'openzeppelin-solidity'
    ];
    
    patterns.forEach(pattern => {
        if (source.includes(pattern)) {
            if (pattern.startsWith('@')) {
                const parts = pattern.split('/');
                if (parts.length >= 2) {
                    packages.push(parts[0] + '/' + parts[1]);
                }
            } else {
                packages.push(pattern);
            }
        }
    });
    
    return packages;
}

function generateVersionAttempts(packages) {
    const attempts = [];
    
    // Generate version combinations for detected packages
    packages.forEach(pkg => {
        if (pkg === '@openzeppelin/contracts' || pkg === 'openzeppelin-solidity') {
            const versions = ['5.0.1', '5.0.0', '4.9.6', '4.9.5', '4.9.4', '3.4.2', '2.5.1'];
            versions.forEach(version => {
                attempts.push({ [pkg]: version });
            });
        } else if (pkg === '@chainlink/contracts') {
            const versions = ['0.0.16', '0.0.15', '0.0.14'];
            versions.forEach(version => {
                attempts.push({ [pkg]: version });
            });
        } else if (pkg === '@uniswap/v3-core') {
            const versions = ['1.0.1', '1.0.0'];
            versions.forEach(version => {
                attempts.push({ [pkg]: version });
            });
        } else if (pkg === '@aave/core-v3') {
            const versions = ['1.19.1', '1.18.0', '1.17.0'];
            versions.forEach(version => {
                attempts.push({ [pkg]: version });
            });
        } else if (pkg === '@compound-finance/compound-protocol') {
            const versions = ['3.1.0', '3.0.0', '2.8.0'];
            versions.forEach(version => {
                attempts.push({ [pkg]: version });
            });
        }
    });
    
    return attempts;
}

function resolveDependency(importPath, versionPins) {
    // Mock dependency resolution
    if (importPath.includes('@openzeppelin/contracts')) {
        return `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Ownable {
    address private _owner;
    
    constructor() {
        _transferOwnership(msg.sender);
    }
    
    modifier onlyOwner() {
        require(owner() == msg.sender, "Ownable: caller is not the owner");
        _;
    }
    
    function owner() public view returns (address) {
        return _owner;
    }
    
    function _transferOwnership(address newOwner) internal virtual {
        _owner = newOwner;
    }
}`;
    }
    
    throw new Error(`Dependency not found: ${importPath}`);
}

function resolveDependencyWithFallback(importPath, versionPins) {
    // Mock CDN fallback
    try {
        return resolveDependency(importPath, versionPins);
    } catch (error) {
        // Try alternative CDN
        return resolveDependency(importPath, versionPins);
    }
}

function verifyContract(source, targetBytecode) {
    // Mock contract verification
    const detectedPackages = detectPackages(source);
    const versionAttempts = generateVersionAttempts(detectedPackages);
    
    // Simulate verification attempts
    for (let i = 0; i < versionAttempts.length; i++) {
        const attempt = versionAttempts[i];
        
        // Mock successful verification for known packages
        if (attempt['@openzeppelin/contracts'] || attempt['openzeppelin-solidity']) {
            return {
                success: true,
                versionPins: attempt,
                attempts: i + 1,
                dependencies: {},
                metadata: {}
            };
        }
    }
    
    return {
        success: false,
        error: 'All verification attempts failed',
        attempts: versionAttempts.length,
        dependencies: {},
        metadata: {}
    };
}

function refreshPackageCache() {
    return true;
}

function getCDNStatus() {
    return {
        'unpkg.com': true,
        'cdn.jsdelivr.net': true,
        'bundle.run': true,
        'esm.sh': true
    };
}

function getRegistryStats() {
    return {
        timestamp: new Date().toISOString(),
        cdnStatus: getCDNStatus()
    };
}

function getPackageInfo(packageName) {
    // Mock package information
    if (packageName === '@openzeppelin/contracts') {
        return {
            name: '@openzeppelin/contracts',
            versions: ['5.0.1', '5.0.0', '4.9.6', '4.9.5', '4.9.4'],
            cdn: 'unpkg.com',
            lastUpdated: new Date().toISOString()
        };
    }
    return null;
}

function generateLargeContract(importCount) {
    let source = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

contract LargeContract {
    uint256 public value;
    
    constructor() {
        value = 42;
    }
    
    function setValue(uint256 newValue) public {
        value = newValue;
    }
}`;
    
    // Add imports
    for (let i = 0; i < importCount; i++) {
        source += `\nimport "@package${i}/contracts/Helper${i}.sol";`;
    }
    
    return { source };
}

console.log('Enhanced Registry System Test Suite Loaded');
console.log('Run with: npm test enhanced-registry-system-test.js');
