#!/bin/sh
# REL-00b: hold graphql-api's build inputs and its ncog-evm sibling to recorded, immutable identities.
#
#   1. every FROM in docker/Dockerfile.apiserver, after ARG-default expansion, pulls <ref>@sha256:<64 hex>
#      -- a tag alone is a pointer upstream moves;
#   2. IF any `apk add` remains, every package in it is pinned to an exact version (name=version);
#   3. ncog-evm.ref records ONE 40-hex commit, and every workflow that checks ncog-evm out reads it
#      (audit O06, second half: this repository used to build against whatever ncog-evm's default branch
#      was that day, so a green run said nothing about any particular ncog-evm);
#   4. when ../ncog-evm is a git checkout (CI, or a developer's sibling), it is AT that commit;
#   5. (REL-05) no package-archive call is made at build time at all, AND the trust store still
#      arrives, by COPY out of a digest-pinned stage. Clause 2 governed an apk call that no longer
#      exists: both of its pins -- ca-certificates=20260909-r0 and tzdata=2026d-r0 -- had aged out
#      of alpine v3.22/main by 2026-11-25, so the image could not be built, and NOTHING REPORTED
#      IT. This script was green throughout, because clause 1 asks whether the FROM lines carry
#      digests (they do) and nothing asked whether a pinned PACKAGE still existed. Alpine serves
#      only the current version per branch, so an exact apk pin cannot be durable here; the fix
#      was to remove the call, not to bump it. Clause 5 is what keeps it removed.
#
# Run from the repository root: sh scripts/check-release-pins.sh
set -u
fail=0
bad() { echo "check-release-pins: FAIL: $*"; fail=1; }
df=docker/Dockerfile.apiserver

args=$(sed -n 's/^ARG \([A-Z_][A-Z0-9_]*\)=\(.*\)$/\1=\2/p' "$df")
expand() {
	out=$1
	for kv in $args; do
		k=${kv%%=*}
		v=${kv#*=}
		out=$(printf '%s' "$out" | sed "s|\${$k}|$v|g")
	done
	printf '%s' "$out"
}

n=0
for ref in $(sed -n 's/^FROM \([^ ]*\).*/\1/p' "$df"); do
	n=$((n + 1))
	img=$(expand "$ref")
	printf '%s' "$img" | grep -Eq '@sha256:[0-9a-f]{64}$' || bad "$df: FROM $img is a tag with no digest"
done
[ "$n" -gt 0 ] || bad "$df: no FROM lines found"

# Everything below reads INSTRUCTIONS, never comments, and that distinction is load-bearing rather
# than tidy. The Dockerfile's own REL-05 note QUOTES the apk line it removed, in order to explain why.
# A whole-file search therefore finds that quotation -- and the quoted tokens ARE pinned, so the pin
# check below would have passed on a sentence in a comment while the image installed nothing at all.
# That is a worse failure than a false red: it is a green check measuring documentation.
code=$(grep -vE '^[[:space:]]*#' "$df")
[ -n "$code" ] || bad "$df: no instructions found after stripping comments; every check below would pass vacuously"

pkgs=$(printf '%s\n' "$code" | sed -n 's/.*apk add \(.*\)$/\1/p')
if [ -n "$pkgs" ]; then
	for tok in $pkgs; do
		case "$tok" in -*) continue ;; esac
		tok=$(expand "$tok")
		printf '%s' "$tok" | grep -Eq '^[a-z0-9][a-z0-9.+_-]*=[0-9A-Za-z.+_~-]+$' || bad "$df: apk add $tok has no exact version"
	done
fi
# A zero-package Dockerfile is the EXPECTED state since REL-05, so the absence of apk is not a failure
# here any more. It used to be, and that was right while the call existed -- it is what stopped anyone
# deleting a pin and leaving a green check behind. The case has MOVED rather than gone: clause 5 below
# asserts both halves of it. Without clause 5, the block above would be vacuous.

# ---- clause 5 (REL-05): reach no package archive, and still ship a trust store -----------------
for pm in 'apk[[:space:]]+(add|update|upgrade)' 'apt-get' '(dnf|yum)[[:space:]]+install' 'pip3?[[:space:]]+install'; do
	hit=$(printf '%s\n' "$code" | grep -nE "$pm" | head -1)
	[ -z "$hit" ] || bad "$df calls a package manager ($hit).
       REL-05 removed the last package-archive call from this image deliberately. A package manager reads
       the live index, which is the one build input the FROM digests cannot cover, and an exact version pin
       there EXPIRES at the next upstream revision -- which is exactly how this image became unbuildable in
       November 2026. If a package is genuinely needed, take it from the digest-pinned builder stage the way
       the trust store is taken, or embed the data in the binary the way time/tzdata is; do not reintroduce
       a pin that rots."
done

certs=$(printf '%s\n' "$code" | grep -cE '^COPY --from=[^ ]+[[:space:]]+/etc/ssl/certs/ca-certificates\.crt[[:space:]]+/etc/ssl/certs/ca-certificates\.crt[[:space:]]*$')
if [ "$certs" -ne 1 ]; then
	bad "$df: expected exactly one COPY --from=<stage> of /etc/ssl/certs/ca-certificates.crt to the same path, found $certs.
       Removing the apk call is only half of REL-05. alpine ships no trust store, and this API dials
       Postgres with sslmode=require/verify-full and downloads solc over HTTPS, so without this COPY the
       image has no roots and fails at RUNTIME -- while a check that only looked for the absence of apk
       would stay green."
else
	stage=$(printf '%s\n' "$code" | sed -n 's/^COPY --from=\([^ ]*\)[[:space:]]*\/etc\/ssl\/certs\/ca-certificates\.crt.*/\1/p')
	printf '%s\n' "$code" | grep -Eq "^FROM .* AS ${stage}\$" \
		|| bad "$df: the trust store is copied from stage '${stage}', which no FROM line defines. The certificates would not be in the image at all."
fi

ref=$( [ -f ncog-evm.ref ] && tr -d ' \r\n' < ncog-evm.ref )
printf '%s' "$ref" | grep -Eq '^[0-9a-f]{40}$' || bad "ncog-evm.ref must hold one 40-hex commit (got '$ref')"

for wf in .github/workflows/*.yml; do
	grep -q 'repository: .*ncog-evm' "$wf" || continue
	grep -q 'cat ncog-evm.ref' "$wf" || bad "$wf checks out ncog-evm but does not read ncog-evm.ref"
	grep -Eq 'ref: \$\{\{ steps\.pin\.outputs\.sha \}\}' "$wf" || bad "$wf checks out ncog-evm without ref: \${{ steps.pin.outputs.sha }}"
done

if [ -d ../ncog-evm/.git ] || [ -f ../ncog-evm/.git ]; then
	head=$(git -C ../ncog-evm rev-parse HEAD 2>/dev/null)
	[ "$head" = "$ref" ] || bad "../ncog-evm is at $head, ncog-evm.ref records $ref: move one to the other"
fi

[ "$fail" -eq 0 ] && echo "check-release-pins: ok (FROM digests, no package-archive call, trust store copied from a pinned stage, ncog-evm at ${ref})"
exit "$fail"
