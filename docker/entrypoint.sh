#!/bin/sh
#
# Container entrypoint for the NCOG Earth Chain GraphQL API server.
#
# WHY THIS SCRIPT EXISTS
# ----------------------
# The application is configured by a FILE and only by a file. internal/config/load.go builds a
# viper reader that searches $HOME/.ncogearthchainapi and "." for a file named "apiserver"
# (internal/config/keys.go:6), or takes an explicit path from the `-cfg` flag. There is no
# environment binding anywhere in the tree -- `grep -rn 'AutomaticEnv\|BindEnv\|SetEnvPrefix'`
# over internal/ and cmd/ returns nothing. A container, meanwhile, is configured by environment.
#
# So this script renders the config file from the environment at start. That keeps the container
# configured EXACTLY the way the application is configured -- same loader, same keys, same
# mapstructure paths -- while giving operators the env-var surface they expect. It deliberately
# does not patch env support into the Go loader: that would change how the binary behaves outside
# a container too, and viper's AutomaticEnv does not reliably reach nested keys through
# Unmarshal.
#
# TWO WAYS TO CONFIGURE, AND THEY DO NOT MIX
# ------------------------------------------
#   1. Mount a complete config at $NEC_API_CONFIG (default /etc/ncog/apiserver.json). If that
#      file exists it is used VERBATIM and nothing below runs. This is the escape hatch for the
#      parts of the config that are not env-shaped -- the governance contract list, the DeFi
#      price symbols, a custom log format.
#   2. Otherwise the environment is rendered into $NEC_API_RENDERED_CONFIG and passed with -cfg.
#
# Viper reads one config file. There is no merging, by design: a half-env/half-file
# configuration is the kind of thing that looks right and behaves like neither.
#
# WHAT IS RENDERED
# ----------------
# Only keys whose environment variable is actually set, plus the five that a container must
# always pin. Everything else is left OUT of the file so the application's own defaults
# (internal/config/default.go) still apply -- writing them out here would silently freeze a copy
# of every default at the version this image was built.
#
set -eu

die() { printf 'apiserver-entrypoint: %s\n' "$*" >&2; exit 1; }
note() { printf 'apiserver-entrypoint: %s\n' "$*" >&2; }

CONFIG_MOUNT=${NEC_API_CONFIG:-/etc/ncog/apiserver.json}
CONFIG_RENDERED=${NEC_API_RENDERED_CONFIG:-/run/ncog/apiserver.json}

# --------------------------------------------------------------------------- JSON helpers --
# jstr escapes a shell value into a JSON string. Control characters are DROPPED rather than
# escaped: no legitimate value here contains one, and a newline smuggled into a DSN would
# otherwise let an environment variable inject arbitrary JSON keys into the rendered config.
jstr() {
    printf '"%s"' "$(printf '%s' "$1" | tr -d '[:cntrl:]' | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g')"
}

# Collectors. Each is a comma-prefixed list of "key":value fragments; obj() strips the leading
# comma and wraps them. Initialised because `set -u` is on.
SRV='' LOG='' PG='' STK='' CMP='' CACHE='' TOP=''

_append() {
    eval "_cur=\${$1}"
    eval "$1=\"\${_cur},\$2\""
}

obj() { printf '{%s}' "${1#,}"; }

# put_str/put_num/put_bool <collector> <json key> <env var name> <value>
# A value that is empty or unset is skipped, which is what leaves the application default in
# force. Validation happens HERE, in the current shell, and not inside a command substitution --
# a `die` in a substitution kills only the subshell and the bad value sails through as empty.
put_str() {
    [ -n "${4:-}" ] || return 0
    _append "$1" "\"$2\":$(jstr "$4")"
}

put_num() {
    [ -n "${4:-}" ] || return 0
    case $4 in ''|*[!0-9]*) die "$3 must be a non-negative integer; got '$4'" ;; esac
    _append "$1" "\"$2\":$4"
}

put_bool() {
    [ -n "${4:-}" ] || return 0
    case $4 in
        true|TRUE|True|1|yes|on)     _v=true ;;
        false|FALSE|False|0|no|off)  _v=false ;;
        *) die "$3 must be true or false; got '$4'" ;;
    esac
    _append "$1" "\"$2\":$_v"
}

# put_csv renders a comma-separated env var as a JSON array of strings.
#
# `set -f` is load-bearing, not defensive habit. The word-splitting below is an UNQUOTED
# expansion, so without it the shell also PATHNAME-expands each field -- and "*" is the default
# and entirely legitimate value of NEC_API_CORS_ORIGINS. The allowed-origins list would silently
# become a list of the filenames in the working directory.
put_csv() {
    [ -n "${4:-}" ] || return 0
    _items=''
    _oldifs=$IFS
    set -f
    IFS=','
    for _it in $4; do
        _it=$(printf '%s' "$_it" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
        [ -n "$_it" ] || continue
        _items="$_items,$(jstr "$_it")"
    done
    IFS=$_oldifs
    set +f
    [ -n "$_items" ] || return 0
    _append "$1" "\"$2\":[${_items#,}]"
}

render_config() {
    # ------------------------------------------------------------------ required inputs --
    # These two have no default for the same reason POSTGRES_PASSWORD has none in
    # docker-compose.ddb.yml: a fallback that happens to work locally is how a deployment
    # quietly points at the wrong database or at no chain at all, and it fails silently.
    [ -n "${NEC_API_PG_URL:-}" ] || die \
"NEC_API_PG_URL is not set.

It is the explorer's PostgreSQL DSN, e.g.
    postgres://explorer:<password>@postgres:5432/nec_explorer?sslmode=disable
Percent-encode any reserved character in the password. This must be a DIFFERENT PostgreSQL
instance from the one a validator runs for its DDB -- see doc/postgres-cutover.md section 0."

    [ -n "${NEC_API_NODE_URL:-}" ] || die \
"NEC_API_NODE_URL is not set.

It is the RPC endpoint of an NCOG Earth Chain node. Accepted forms (go-ethereum rpc.Dial):
    http://host:18545        an HTTP RPC endpoint reachable from this container
    ws://host:18546          a WebSocket RPC endpoint
    /var/run/ncog/ncog.ipc   a UNIX socket, which must be bind-mounted into the container
For a node running on the container host, use http://host.docker.internal:18545 and give the
container 'host.docker.internal:host-gateway' in extra_hosts (docker-compose.yml already does)."

    # ------------------------------------------------------------------------- server --
    # bind is always written: the application default is localhost:16761
    # (internal/config/default.go), which inside a container listens where nothing can reach it.
    put_str  SRV bind                 NEC_API_BIND                 "${NEC_API_BIND:-0.0.0.0:16761}"
    put_str  SRV domain               NEC_API_DOMAIN               "${NEC_API_DOMAIN:-}"
    put_str  SRV origin               NEC_API_ORIGIN               "${NEC_API_ORIGIN:-}"
    put_csv  SRV cors_origins         NEC_API_CORS_ORIGINS         "${NEC_API_CORS_ORIGINS:-}"
    put_csv  SRV peers                NEC_API_PEERS                "${NEC_API_PEERS:-}"
    put_num  SRV read_timeout         NEC_API_READ_TIMEOUT         "${NEC_API_READ_TIMEOUT:-}"
    put_num  SRV write_timeout        NEC_API_WRITE_TIMEOUT        "${NEC_API_WRITE_TIMEOUT:-}"
    put_num  SRV idle_timeout         NEC_API_IDLE_TIMEOUT         "${NEC_API_IDLE_TIMEOUT:-}"
    put_num  SRV header_timeout       NEC_API_HEADER_TIMEOUT       "${NEC_API_HEADER_TIMEOUT:-}"
    put_num  SRV resolver_timeout     NEC_API_RESOLVER_TIMEOUT     "${NEC_API_RESOLVER_TIMEOUT:-}"
    put_num  SRV max_query_depth      NEC_API_MAX_QUERY_DEPTH      "${NEC_API_MAX_QUERY_DEPTH:-}"
    put_num  SRV max_query_complexity NEC_API_MAX_QUERY_COMPLEXITY "${NEC_API_MAX_QUERY_COMPLEXITY:-}"
    put_num  SRV max_request_body     NEC_API_MAX_REQUEST_BODY     "${NEC_API_MAX_REQUEST_BODY:-}"
    put_num  SRV max_parallelism      NEC_API_MAX_PARALLELISM      "${NEC_API_MAX_PARALLELISM:-}"
    put_bool SRV graphi_enabled       NEC_API_GRAPHI_ENABLED       "${NEC_API_GRAPHI_ENABLED:-}"

    # --------------------------------------------------------------------------- log --
    # output defaults to stdout here rather than the application's stderr: `docker logs` and
    # every log shipper treat stdout as the application stream.
    put_str  LOG level  NEC_API_LOG_LEVEL  "${NEC_API_LOG_LEVEL:-INFO}"
    put_str  LOG output NEC_API_LOG_OUTPUT "${NEC_API_LOG_OUTPUT:-stdout}"
    put_str  LOG format NEC_API_LOG_FORMAT "${NEC_API_LOG_FORMAT:-}"

    # ---------------------------------------------------------------------- postgres --
    put_str  PG url               NEC_API_PG_URL               "$NEC_API_PG_URL"
    put_num  PG max_conns         NEC_API_PG_MAX_CONNS         "${NEC_API_PG_MAX_CONNS:-}"
    put_num  PG min_conns         NEC_API_PG_MIN_CONNS         "${NEC_API_PG_MIN_CONNS:-}"
    put_num  PG statement_timeout NEC_API_PG_STATEMENT_TIMEOUT "${NEC_API_PG_STATEMENT_TIMEOUT:-}"
    put_bool PG auto_migrate      NEC_API_PG_AUTO_MIGRATE      "${NEC_API_PG_AUTO_MIGRATE:-}"

    # ----------------------------------------------------------------------- staking --
    put_str  STK sfc NEC_API_SFC_CONTRACT "${NEC_API_SFC_CONTRACT:-}"
    put_str  STK sti NEC_API_STI_CONTRACT "${NEC_API_STI_CONTRACT:-}"

    # ---------------------------------------------------------------------- compiler --
    # solc is NOT in this image: it is needed only by the contract-verification mutation, it is
    # a large per-version download, and internal/solidity resolves and fetches versions at run
    # time. Point these at a mounted volume if you serve verification.
    put_str  CMP sol  NEC_API_SOLC_PATH     "${NEC_API_SOLC_PATH:-}"
    put_str  CMP temp NEC_API_COMPILER_TEMP "${NEC_API_COMPILER_TEMP:-}"

    # ------------------------------------------------------------------------- cache --
    put_num  CACHE size     NEC_API_CACHE_SIZE     "${NEC_API_CACHE_SIZE:-}"
    put_str  CACHE eviction NEC_API_CACHE_EVICTION "${NEC_API_CACHE_EVICTION:-}"

    # -------------------------------------------------------------------- top level --
    put_str TOP app_name NEC_API_APP_NAME "${NEC_API_APP_NAME:-}"
    # Always absolute: the application default is the RELATIVE path "tokens.json", which
    # resolves against the working directory and quietly yields no logos if that ever changes.
    put_str TOP erc20_tokens_file NEC_API_ERC20_TOKENS_FILE "${NEC_API_ERC20_TOKENS_FILE:-/app/tokens.json}"

    _me=''
    put_str _me address NEC_API_ME_ADDRESS "${NEC_API_ME_ADDRESS:-}"

    _repo=''
    put_bool _repo stakers NEC_API_MONITOR_STAKERS "${NEC_API_MONITOR_STAKERS:-}"

    _node=''
    put_str _node url NEC_API_NODE_URL "$NEC_API_NODE_URL"

    _dir=$(dirname "$CONFIG_RENDERED")
    mkdir -p "$_dir" 2>/dev/null || die "cannot create $_dir -- with --read-only, mount a tmpfs there"

    # 0600 before a single byte is written. The file carries the PostgreSQL password.
    ( umask 077
      {
        printf '{'
        printf '"server":%s'   "$(obj "$SRV")"
        printf ',"log":%s'     "$(obj "$LOG")"
        printf ',"pg":%s'      "$(obj "$PG")"
        printf ',"node":%s'    "$(obj "$_node")"
        [ -z "$STK" ]   || printf ',"staking":%s'    "$(obj "$STK")"
        [ -z "$CMP" ]   || printf ',"compiler":%s'   "$(obj "$CMP")"
        [ -z "$CACHE" ] || printf ',"cache":%s'      "$(obj "$CACHE")"
        [ -z "$_me" ]   || printf ',"me":%s'         "$(obj "$_me")"
        [ -z "$_repo" ] || printf ',"repository":%s' "$(obj "$_repo")"
        [ -z "$TOP" ]   || printf '%s'               "$TOP"
        printf '}\n'
      } > "$CONFIG_RENDERED" ) || die "cannot write $CONFIG_RENDERED"

    # Report what was configured WITHOUT the DSN password. The host/db is the useful part and
    # the credential is the part that must never reach a log aggregator.
    note "rendered $CONFIG_RENDERED from the environment"
    note "  node.url   = $NEC_API_NODE_URL"
    note "  pg.url     = $(printf '%s' "$NEC_API_PG_URL" | sed -e 's#://[^:/@]*:[^@]*@#://***:***@#')"
    note "  server.bind= ${NEC_API_BIND:-0.0.0.0:16761}"
}

# ------------------------------------------------------------------------------ dispatch --
case "${1:-apiserver}" in
    apiserver)
        if [ -f "$CONFIG_MOUNT" ]; then
            note "using mounted configuration $CONFIG_MOUNT (environment configuration ignored)"
            exec /usr/local/bin/apiserver -cfg "$CONFIG_MOUNT"
        fi
        render_config
        exec /usr/local/bin/apiserver -cfg "$CONFIG_RENDERED"
        ;;

    migrate)
        # Applies the schema through the same embedded migrations the server runs at startup.
        # Useful when NEC_API_PG_AUTO_MIGRATE is false and migrations are a deliberate step.
        # It reads the DSN from the environment because it has no config-file loader of its own,
        # so a MOUNTED config does not supply it.
        [ -n "${NEC_API_PG_URL:-}" ] || die "migrate needs NEC_API_PG_URL (it does not read the config file)"
        shift
        exec /usr/local/bin/migrate -dsn "$NEC_API_PG_URL" "$@"
        ;;

    version)
        # `apiserver -v` is NOT a version check that can run without a backend: config.Load()
        # happens in init() and resolvers.New() -> svc.Manager() -> repository.R() opens both
        # PostgreSQL and the node RPC before run() ever looks at the flag. The build metadata is
        # therefore reported from the image label instead.
        cat /etc/ncog-apiserver-version 2>/dev/null || echo "unknown"
        ;;

    *)
        exec "$@"
        ;;
esac
