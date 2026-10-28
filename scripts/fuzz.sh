#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# fuzz.sh runs every Fuzz target in the module for FUZZTIME each
# (default 15s). Targets are discovered with `go test -list`, so new
# targets are picked up automatically. Optional arguments restrict the
# run to the given packages. Works with bash 3.2 (macOS).
#
# Usage: FUZZTIME=2s scripts/fuzz.sh [./pkg ...]
set -euo pipefail

fuzztime="${FUZZTIME:-15s}"
if [ "$#" -gt 0 ]; then
	pkgs="$*"
else
	pkgs=$(go list ./... | grep -Ev '/examples($|/)')
fi

count=0
for pkg in $pkgs; do
	for target in $(go test -list '^Fuzz' "$pkg" | grep '^Fuzz' || true); do
		echo "=== $pkg $target ($fuzztime)"
		# On a slow machine the fuzz coordinator can time out on a short
		# -fuzztime without finding anything ("context deadline exceeded",
		# no failing input). Retry that case once; real failures write a
		# failing input to testdata and are never retried.
		if ! out=$(go test -run='^$' -fuzz="^${target}\$" -fuzztime="$fuzztime" "$pkg" 2>&1); then
			if grep -q 'context deadline exceeded' <<<"$out" && ! grep -q 'Failing input written' <<<"$out"; then
				echo "$out" | tail -3
				echo "--- retrying $target (fuzz coordinator timeout)"
				go test -run='^$' -fuzz="^${target}\$" -fuzztime="$fuzztime" "$pkg"
			else
				echo "$out"
				exit 1
			fi
		else
			echo "$out" | tail -2
		fi
		count=$((count + 1))
	done
done
echo "fuzzed $count targets for $fuzztime each"
