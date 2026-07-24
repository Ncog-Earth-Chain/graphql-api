// Package config handles API server configuration binding and loading.
package config

// default configuration elements and keys
const (
	configFileName = "apiserver"

	// configuration options
	keyAppName                  = "app_name"
	keyConfigFilePath           = "cfg"
	keyConfigCmdBlockScanStart  = "cmd.blk_from"
	keyConfigCmdBlockScanEnd    = "cmd.blk_to"
	keyConfigCmdBlockScanReScan = "cmd.rescan"
	keyConfigCmdRestoreStake    = "cmd.fix_stake"

	// server related keys
	keyBindAddress      = "server.bind"
	keyDomainAddress    = "server.domain"
	keyApiPeers         = "server.peers"
	keyApiStateOrigin   = "server.origin"
	keyCorsAllowOrigins = "server.cors_origins"

	// server time out related keys
	keyTimeoutRead     = "server.read_timeout"
	keyTimeoutWrite    = "server.write_timeout"
	keyTimeoutIdle     = "server.idle_timeout"
	keyTimeoutHeader   = "server.header_timeout"
	keyTimeoutResolver = "server.resolver_timeout"

	// query abuse protection keys
	keyMaxQueryDepth   = "server.max_query_depth"
	keyMaxQueryComplex = "server.max_query_complexity"
	keyMaxRequestBody  = "server.max_request_body"
	keyMaxParallelism  = "server.max_parallelism"
	keyGraphiEnabled   = "server.graphi_enabled"

	// API server identity key. There is no private-key option: the API server
	// is a read-only observer and must never hold signing material.
	keySignatureAddress = "me.address"

	// logging related options
	keyLoggingLevel  = "log.level"
	keyLoggingFormat = "log.format"

	// Node connection.
	//
	// This MUST byte-match the mapstructure path of Config.Forest, which is tagged
	// `mapstructure:"node"` -- so the path is "node.url", not "forest.url". It was
	// "forest.url", which meant SetDefault wrote a key nothing ever read: the
	// documented default IPC path never reached cfg.Forest.Url, and an operator who
	// omitted node.url from their config got an empty dial string instead of the
	// default. The deployed example config uses "node", confirming which side is wrong.
	//
	// A key constant that does not match its mapstructure path produces a silently
	// inert default -- no error, no warning, just a zero value. See config_test.go,
	// which asserts every key resolves to a real field.
	keyForestUrl = "node.url"

	// PostgreSQL -- the explorer's storage
	keyPgUrl              = "pg.url"
	keyPgMaxConns         = "pg.max_conns"
	keyPgMinConns         = "pg.min_conns"
	keyPgStatementTimeout = "pg.statement_timeout"
	keyPgAutoMigrate      = "pg.auto_migrate"

	// cache related options
	keyCacheEvictionTime = "cache.eviction"
	keyCacheMaxSize      = "cache.size"

	// contract validation related
	keySolCompilerPath  = "compiler.sol"
	keyCompilerTempPath = "compiler.temp"

	// utility options
	keyVotingSources         = "voting.sources"
	keyErc20TokenMapFilePath = "erc20_tokens_file"
	keyErc20Logos            = "erc20_logos"

	// PoS staking configuration
	keyStakingSfcContract = "staking.sfc"
	keyStakingStiContract = "staking.sti"
	//keyStakingTokenizerContract = "staking.tokenizer"
	//keyStakingERC20Token        = "staking.token"
)
