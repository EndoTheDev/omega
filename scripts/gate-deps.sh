#!/usr/bin/env bash
set -euo pipefail

# Dependency-law gate: llm/ and agent/ never import harness/, directly or
# transitively. Modules may import agent and ai; the layers below never
# reach back. Standalone or in the build.sh cascade (after vet).
#
# Self-test override: OMEGA_GATE_FIXTURE=<file> reads synthetic cascade
# data instead of querying go list. Two line shapes:
#   PKG <importpath>|<imports>|<testimports>   direct imports of a layer package
#   <importpath>                               a path in the transitive dep closure
# See gate-deps_testdata/ for the fixtures.

# The script lives in scripts/ but the dep-law scan runs against the
# module root, so anchor there regardless of the caller's CWD.
cd "$(dirname "$0")/.."
MODROOT="$(go list -m -f '{{.Path}}')"

violations=0
violation() {
	echo "dep-law violation: $1"
	violations=$((violations + 1))
}

gen() {
	# -test pulls in each layer package's test files; second call yields
	# the flat transitive closure of llm/ and agent/.
	go list -test -f \
		'{{.ImportPath}}|{{join .Imports " "}}|{{join .TestImports " "}}' \
		./llm/... ./agent/... &&
	go list -deps -test ./llm/... ./agent/...
}

scan() {
	local line pkg fields field imp
	while IFS= read -r line; do
		[ -z "$line" ] && continue
		case "$line" in
			*"|"*)
				pkg=${line%%|*}
				pkg=${pkg#PKG }
				fields=${line#*|}
				for field in ${fields//|/ }; do
					for imp in $field; do
						case "$imp" in
							"$MODROOT"/harness/*)
								violation "$pkg imports $imp" ;;
						esac
					done
				done ;;
			"$MODROOT"/harness/*)
				violation "engine llm/agent dep closure includes $line" ;;
		esac
	done
}

if [[ -n ${OMEGA_GATE_FIXTURE:-} ]]; then
	scan < "$OMEGA_GATE_FIXTURE"
else
	output="$(gen)"
	scan <<< "$output"
fi

if [ "$violations" -gt 0 ]; then
	echo "$violations dep-law violation(s): harness/ never appears in the llm/agent import graph"
	exit 1
fi