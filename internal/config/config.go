// Package config handles API server configuration binding and loading.
package config

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Config defines configuration options structure for NCOGEarthChain API server.
type Config struct {
	// AppName holds the name of the application
	AppName string `mapstructure:"app_name"`

	// MySignature represents a signature of the server on blockchain.
	MySignature ServerSignature `mapstructure:"me"`

	// Server configuration
	Server Server `mapstructure:"server"`

	// Logger configuration
	Log Log `mapstructure:"log"`

	// Forest represents the node structure
	Forest Forest `mapstructure:"node"`

	// PostgreSQL configuration -- the explorer's storage.
	Pg Postgres `mapstructure:"pg"`

	// Cache configuration
	Cache Cache `mapstructure:"cache"`

	// Cache configuration
	Compiler Compiler `mapstructure:"compiler"`

	// Repository configuration
	Repository Repository `mapstructure:"repository"`

	// Staking configuration
	Staking Staking `mapstructure:"staking"`

	// DeFi configuration
	DeFi DeFi `mapstructure:"defi"`

	// Governance configuration
	Governance Governance `mapstructure:"governance"`

	// TokenLogoFilePath contains the path to JSON file with the map
	// of known ERC20 tokens to their logo URLs.
	// The file will be loaded on configuration loading.
	TokenLogoFilePath string `mapstructure:"erc20_tokens_file"`

	// TokenLogo is a list of known ERC20 tokens
	// mapped to URL addresses of their logos.
	TokenLogo map[common.Address]string

	// ReScanBlocks represents the number of blocks to be re-scanned.
	RepoCommand RepoCmd `mapstructure:"cmd"`
}

// RepoCmd represents a repository command configuration.
type RepoCmd struct {
	BlockScanReScan uint64
	RestoreStake    string
}

// Server represents the GraphQL server configuration
type Server struct {
	BindAddress     string   `mapstructure:"bind"`
	DomainAddress   string   `mapstructure:"domain"`
	Origin          string   `mapstructure:"origin"`
	Peers           []string `mapstructure:"peers"`
	CorsOrigin      []string `mapstructure:"cors_origins"`
	ReadTimeout     int64    `mapstructure:"read_timeout"`
	WriteTimeout    int64    `mapstructure:"write_timeout"`
	IdleTimeout     int64    `mapstructure:"idle_timeout"`
	HeaderTimeout   int64    `mapstructure:"header_timeout"`
	ResolverTimeout int64    `mapstructure:"resolver_timeout"`

	// Query abuse protection. The schema is cyclic (Block.txList -> Transaction.block
	// -> txList, and Block.parent -> Block), so an unbounded query can be made to
	// multiply work without limit. These bound it.
	MaxQueryDepth      int   `mapstructure:"max_query_depth"`
	MaxQueryComplexity int   `mapstructure:"max_query_complexity"`
	MaxRequestBody     int64 `mapstructure:"max_request_body"`
	MaxParallelism     int   `mapstructure:"max_parallelism"`

	// GraphiEnabled controls whether the interactive GraphiQL IDE is served.
	// It is unauthenticated, so it defaults to off outside development.
	GraphiEnabled bool `mapstructure:"graphi_enabled"`
}

// ServerSignature represents the identity used by this server
// when it addresses the blockchain, i.e. the `from` of read-only contract calls.
//
// There is deliberately NO private key here. The API server is a read-only
// observer: it never signs anything, so it must never hold signing material.
// A `me.pkey` option used to exist and was never read by any code path.
type ServerSignature struct {
	Address common.Address `mapstructure:"address"`
}

// Log represents the logger configuration
type Log struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// Forest represents the Forest node access configuration
type Forest struct {
	Url string `mapstructure:"url"`
}

// Postgres represents the PostgreSQL connection configuration.
//
// PostgreSQL replaces MongoDB as the explorer's storage. It is deliberately a SEPARATE
// instance from the one the chain node runs for its DDB: DDB rows are consensus state,
// explorer rows are a rebuildable index, and pg_wal, disk, max_connections and the
// postmaster lifetime are all cluster-scoped. An explorer re-scan filling the disk must
// not be able to take the validator's database down with it.
type Postgres struct {
	// Url is the libpq connection string or postgres:// URL.
	Url string `mapstructure:"url"`

	// MaxConns bounds the pool. Sized against the server's max_connections rather than
	// against expected concurrency: the ingest path can spawn work per transaction, and
	// an unbounded pool turns that into connection exhaustion that locks out the
	// operator's own session.
	MaxConns int32 `mapstructure:"max_conns"`

	// MinConns keeps warm connections so a cold pool does not show up as latency on the
	// first requests after an idle period.
	MinConns int32 `mapstructure:"min_conns"`

	// StatementTimeout bounds any single statement server-side, in seconds. Context
	// cancellation covers the client side; this covers a client that vanished without
	// cancelling and left the server working.
	StatementTimeout int64 `mapstructure:"statement_timeout"`

	// AutoMigrate applies pending schema migrations at startup. Serving requests against
	// a half-migrated schema produces errors that look like data corruption, so this is
	// fatal on failure.
	AutoMigrate bool `mapstructure:"auto_migrate"`
}

// Cache represents the cache sub-system configuration.
type Cache struct {
	Eviction time.Duration `mapstructure:"eviction"`
	MaxSize  int           `mapstructure:"size"`
}

// Compiler represents the contract compilers configuration.
type Compiler struct {
	CompilerTempPath       string `mapstructure:"temp"`
	DefaultSolCompilerPath string `mapstructure:"sol"`
}

// Repository represents the repository configuration.
type Repository struct {
	MonitorStakers bool `mapstructure:"stakers"`
}

// Staking represents the PoS Staking module configuration.
type Staking struct {
	SFCContract         common.Address `mapstructure:"sfc"`
	StiContract         common.Address `mapstructure:"sti"`
	TokenizerContract   common.Address `mapstructure:"tokenizer"`
	TokenizedStakeToken common.Address `mapstructure:"token"`
}

// DeFi represents the DeFi and financial contracts configuration.
type DeFi struct {
	FMint        DeFiFMint `mapstructure:"fmint"`
	PriceSymbols []string  `mapstructure:"symbols"`
}

// DeFiFMint represents the fMint DeFi module configuration.
type DeFiFMint struct {
	AddressProvider common.Address `mapstructure:"address_provider"`
}

// Governance represents the governance module configuration.
type Governance struct {
	Contracts []GovernanceContract `mapstructure:"contracts"`
}

// GovernanceContract represents a single Governance contract configuration.
type GovernanceContract struct {
	Address    common.Address `mapstructure:"address"`
	Governable common.Address `mapstructure:"governable"`
	Templates  common.Address `mapstructure:"templates"`
	Name       string         `mapstructure:"name"`
	Type       string         `mapstructure:"type"`
}
