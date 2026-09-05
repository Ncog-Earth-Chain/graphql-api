#!/bin/sh
#
# Container health probe for the NCOG Earth Chain GraphQL API server.
#
# WHAT IS BEING CHECKED, AND WHY IT IS NOT A PORT CHECK
# -----------------------------------------------------
# The listener comes up before the backends are usable and stays up after they fail, so
# "port 16761 accepts a connection" is true across most of the states an operator cares about.
# In particular a `depends_on: service_healthy` gate built on a TCP check would release traffic
# to an API that answers every query with an error.
#
# /health (internal/handlers/health.go) is the application's own readiness endpoint. It calls
# repository.Healthy(), which PINGS PostgreSQL and then READS the ingest watermark
# (meta_counter.contiguous_head) -- so it exercises the connection pool, the schema and a real
# query, bounded by a 3s server-side timeout. It answers 200 with {"contiguousHead":N,
# "status":"ok"} or 503 with {"status":"unavailable"}.
#
# Two assertions, because either alone is insufficient:
#   1. wget's exit status, which is non-zero on the 503. This catches "database gone".
#   2. the body actually being status:ok. This catches a 200 that is not this endpoint's 200 --
#      an intercepting proxy's error page, a truncated body from a write-deadline cut, a future
#      handler that reports degraded state with a 200.
#
# It probes 127.0.0.1 rather than the published address on purpose: this runs INSIDE the
# container and must not depend on the port publication or the bridge network being right.
set -eu

BIND=${NEC_API_BIND:-0.0.0.0:16761}

# Take the port from the last colon-separated field so an IPv6 bind like [::]:16761 works too.
PORT=${BIND##*:}
case $PORT in
    ''|*[!0-9]*) echo "healthcheck: cannot read a port out of NEC_API_BIND='$BIND'" >&2; exit 1 ;;
esac

URL="http://127.0.0.1:${PORT}/health"

if ! BODY=$(wget -q -T 4 -O - "$URL" 2>/dev/null); then
    echo "healthcheck: $URL did not return success (backend unavailable, or not listening)" >&2
    exit 1
fi

case $BODY in
    *'"status":"ok"'*) exit 0 ;;
    *) echo "healthcheck: $URL answered 200 but the body is not this API's health payload: $BODY" >&2
       exit 1 ;;
esac
