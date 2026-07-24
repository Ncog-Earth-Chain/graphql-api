package config

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/viper"
)

// Default values of configuration options
const (
	// this defines default application name
	defApplicationName = "NCOGEarthChain GraphQL API Server (custom)"

	// defSelfAddress is a default address used as a placeholder
	// for actual API server identification.
	// Please make sure to configure your real key for your API server on the wild.
	defSelfAddress = "0xfafe48d3498a97cb6aba4a064f1fb1816ab95684"

	// EmptyAddress defines an empty address
	EmptyAddress = "0x0000000000000000000000000000000000000000"

	// defServerBind holds default API server binding address
	defServerBind = "localhost:16761"

	// default set of timeouts for the server, in seconds
	//
	// WriteTimeout MUST exceed ResolverTimeout. http.TimeoutHandler needs to still
	// own the connection when it fires, otherwise the server's own write deadline
	// cuts the socket first and the client gets a truncated response instead of the
	// intended "Service timeout." body. This is enforced at startup, see app.go.
	defReadTimeout     = 2
	defWriteTimeout    = 35
	defIdleTimeout     = 1
	defHeaderTimeout   = 1
	defResolverTimeout = 30

	// defMaxQueryDepth bounds GraphQL query nesting. The schema is cyclic
	// (Block.txList -> Transaction.block -> txList; Block.parent -> Block), so
	// without this a single query can multiply node work without limit.
	defMaxQueryDepth = 12

	// defMaxQueryComplexity bounds the estimated field-resolution count of a query,
	// i.e. the product of list sizes along each path. Depth alone does not stop a
	// shallow query that asks for 250 transactions x their internal traces.
	defMaxQueryComplexity = 25000

	// defMaxRequestBody caps the accepted request body at 1 MiB. Queries are text;
	// anything larger is abuse rather than use.
	defMaxRequestBody = 1 << 20

	// defMaxParallelism bounds concurrent resolver goroutines per query.
	defMaxParallelism = 10

	// defGraphiEnabled keeps the unauthenticated GraphiQL IDE off by default.
	// Turn it on deliberately in a development configuration.
	defGraphiEnabled = false

	// defServerDomain holds default API server domain address
	defServerDomain = "localhost:16761"

	// defLoggingLevel holds default Logging level
	// See `godoc.org/github.com/op/go-logging` for the full format specification
	// See `golang.org/pkg/time/` for time format specification
	defLoggingLevel = "INFO"

	// defLoggingFormat holds default format of the Logger output
	defLoggingFormat = "%{color}%{level:-8s} %{shortpkg}/%{shortfunc}%{color:reset}: %{message}"

	// defForestUrl holds default Forest connection string
	defForestUrl = "~/.ncogearthchain/ncogearthchain.ipc"

	// PostgreSQL defaults. No password in the default DSN -- an operator must supply a
	// real connection string, and a default that happens to work against a local
	// throwaway instance is how a deployment quietly points at the wrong database.
	defPgUrl              = "postgres://explorer@localhost:5432/nec_explorer?sslmode=disable"
	defPgMaxConns         = 16
	defPgMinConns         = 2
	defPgStatementTimeout = 30
	defPgAutoMigrate      = true

	// defCacheEvictionTime holds default time for in-memory eviction periods
	defCacheEvictionTime = 15 * time.Minute

	// defCacheMax size represents the default max size of the cache in MB
	defCacheMaxSize = 4096

	// defSolCompilerPath represents the default SOL compiler path
	defSolCompilerPath = "/usr/bin/solc"

	// defCompilerTempPath represents the default compiler temp directory
	defCompilerTempPath = "/tmp/solidity"

	// defApiStateOrigin represents the default origin used for API state syncing
	defApiStateOrigin = "https://localhost"

	// defSfcContract is the default address of the SFC contract
	defSfcContract = "0xFC00FACE00000000000000000000000000000000"

	// defStiContract holds deployment address of the Staker Info smart contract.
	defStiContract = EmptyAddress

	// defTokenLogoFilePath represents the default path to the tokens map file
	defTokenLogoFilePath = "tokens.json"

	// defBlockScanRescanDepth represents the amount of blocks re-scanned on server start
	defBlockScanRescanDepth = 200
)

// default list of API peers
var defApiPeers = []string{"https://localhost:3000/api"}

// defCorsAllowOrigins holds CORS default allowed origins.
var defCorsAllowOrigins = []string{"*"}

// default list of API peers
var defVotingSources = make([]string, 0)

// defERC20Logo defines default no-URL value for ERC20 logo list
var defERC20Logo = map[common.Address]string{
	common.HexToAddress(EmptyAddress): "https://repository.ncogchain.earth/logos/erc20.svg",
}

// applyDefaults sets default values for configuration options.
func applyDefaults(cfg *viper.Viper) {
	// set simple details
	cfg.SetDefault(keyAppName, defApplicationName)
	cfg.SetDefault(keyBindAddress, defServerBind)
	cfg.SetDefault(keyDomainAddress, defServerDomain)
	cfg.SetDefault(keySignatureAddress, defSelfAddress)
	cfg.SetDefault(keyLoggingLevel, defLoggingLevel)
	cfg.SetDefault(keyLoggingFormat, defLoggingFormat)
	cfg.SetDefault(keyForestUrl, defForestUrl)

	// PostgreSQL
	cfg.SetDefault(keyPgUrl, defPgUrl)
	cfg.SetDefault(keyPgMaxConns, defPgMaxConns)
	cfg.SetDefault(keyPgMinConns, defPgMinConns)
	cfg.SetDefault(keyPgStatementTimeout, defPgStatementTimeout)
	cfg.SetDefault(keyPgAutoMigrate, defPgAutoMigrate)
	cfg.SetDefault(keySolCompilerPath, defSolCompilerPath)
	cfg.SetDefault(keyCompilerTempPath, defCompilerTempPath)
	cfg.SetDefault(keyApiPeers, defApiPeers)
	cfg.SetDefault(keyApiStateOrigin, defApiStateOrigin)
	cfg.SetDefault(keyErc20TokenMapFilePath, defTokenLogoFilePath)
	cfg.SetDefault(keyErc20Logos, defERC20Logo)

	// in-memory cache
	cfg.SetDefault(keyCacheEvictionTime, defCacheEvictionTime)
	cfg.SetDefault(keyCacheMaxSize, defCacheMaxSize)

	// server timeouts
	cfg.SetDefault(keyTimeoutRead, defReadTimeout)
	cfg.SetDefault(keyTimeoutWrite, defWriteTimeout)
	cfg.SetDefault(keyTimeoutHeader, defHeaderTimeout)
	cfg.SetDefault(keyTimeoutIdle, defIdleTimeout)
	cfg.SetDefault(keyTimeoutResolver, defResolverTimeout)

	// query abuse protection
	cfg.SetDefault(keyMaxQueryDepth, defMaxQueryDepth)
	cfg.SetDefault(keyMaxQueryComplex, defMaxQueryComplexity)
	cfg.SetDefault(keyMaxRequestBody, defMaxRequestBody)
	cfg.SetDefault(keyMaxParallelism, defMaxParallelism)
	cfg.SetDefault(keyGraphiEnabled, defGraphiEnabled)

	// no voting sources by default
	cfg.SetDefault(keyVotingSources, defVotingSources)

	// cors
	cfg.SetDefault(keyCorsAllowOrigins, defCorsAllowOrigins)

	// staking configuration defaults
	cfg.SetDefault(keyStakingSfcContract, defSfcContract)
	cfg.SetDefault(keyStakingStiContract, defStiContract)
	//cfg.SetDefault(keyStakingTokenizerContract, EmptyAddress)
	//cfg.SetDefault(keyStakingERC20Token, EmptyAddress)
}
