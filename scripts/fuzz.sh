#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# fuzz.sh runs every Fuzz target in the module for FUZZTIME each
# (default 15s). Targets are discovered with `go test -list`, so new
# targets are picked up automatically. Optional arguments restrict the
# run to the given packages. Works with bash 3.2 (macOS).
#
# FUZZ_SHARD=i/n runs only the i-th of every n targets (1-based, in
# discovery order), so a CI matrix can split the run.
#
# Usage: FUZZTIME=2s [FUZZ_SHARD=1/3] scripts/fuzz.sh [./pkg ...]
set -euo pipefail

fuzztime="${FUZZTIME:-15s}"
if [ "$#" -gt 0 ]; then
	pkgs="$*"
else
	pkgs=$(go list ./... | grep -Ev '/examples($|/)')
fi

shard="${FUZZ_SHARD:-1/1}"
shard_i="${shard%/*}"
shard_n="${shard#*/}"
case "$shard_i/$shard_n" in
*[!0-9/]* | /* | */) echo "FUZZ_SHARD must be i/n, got $shard" >&2; exit 2 ;;
esac
if [ "$shard_i" -lt 1 ] || [ "$shard_i" -gt "$shard_n" ]; then
	echo "FUZZ_SHARD must be i/n with 1 <= i <= n, got $shard" >&2
	exit 2
fi

count=0
seen=0
for pkg in $pkgs; do
	for target in $(go test -list '^Fuzz' "$pkg" | grep '^Fuzz' || true); do
		seen=$((seen + 1))
		if [ $(((seen - 1) % shard_n + 1)) -ne "$shard_i" ]; then
			continue
		fi
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
echo "fuzzed $count of $seen targets for $fuzztime each (shard $shard)"
