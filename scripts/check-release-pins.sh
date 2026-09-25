#!/bin/sh
# REL-00b: hold graphql-api's build inputs and its ncog-evm sibling to recorded, immutable identities.
#
#   1. every FROM in docker/Dockerfile.apiserver, after ARG-default expansion, pulls <ref>@sha256:<64 hex>
#      -- a tag alone is a pointer upstream moves;
#   2. every `apk add` package is pinned to an exact version (name=version);
#   3. ncog-evm.ref records ONE 40-hex commit, and every workflow that checks ncog-evm out reads it
#      (audit O06, second half: this repository used to build against whatever ncog-evm's default branch
#      was that day, so a green run said nothing about any particular ncog-evm);
#   4. when ../ncog-evm is a git checkout (CI, or a developer's sibling), it is AT that commit.
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

pkgs=$(sed -n 's/.*apk add \(.*\)$/\1/p' "$df")
[ -n "$pkgs" ] || bad "$df: no apk add found; if packages are installed another way, pin them there and move this check"
for tok in $pkgs; do
	case "$tok" in -*) continue ;; esac
	tok=$(expand "$tok")
	printf '%s' "$tok" | grep -Eq '^[a-z0-9][a-z0-9.+_-]*=[0-9A-Za-z.+_~-]+$' || bad "$df: apk add $tok has no exact version"
done

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

[ "$fail" -eq 0 ] && echo "check-release-pins: ok (FROM digests, apk versions, ncog-evm at ${ref})"
exit "$fail"
