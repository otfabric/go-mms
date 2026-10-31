#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# coverage-gate.sh runs the tests with a coverage profile and fails when
# the total or any package with statements is below COVER_PKG_MIN percent
# (default 90). Example programs are excluded. Works with bash 3.2.
#
# Usage: scripts/coverage-gate.sh [profile]   (default: coverage.out)
set -euo pipefail

min="${COVER_PKG_MIN:-90}"
profile="${1:-coverage.out}"

pkgs=$(go list ./... | grep -Ev '/examples($|/)')
# shellcheck disable=SC2086 # word splitting of the package list is intended
go test -count=1 -covermode=set -coverprofile="$profile" $pkgs >/dev/null

awk -v min="$min" '
NR == 1 { next }
{
	split($1, loc, ":")
	file = loc[1]
	pkg = file
	sub(/\/[^\/]*$/, "", pkg)
	if (!(pkg in stmts)) order[++n] = pkg
	stmts[pkg] += $2
	if ($3 > 0) covered[pkg] += $2
	all += $2
	if ($3 > 0) allCovered += $2
}
END {
	fail = 0
	for (i = 1; i <= n; i++) {
		p = order[i]
		pct = 100 * covered[p] / stmts[p]
		status = "ok"
		if (pct < min) { status = "FAIL"; fail = 1 }
		printf "%-6s %6.1f%%  %s\n", status, pct, p
	}
	total = 100 * allCovered / all
	status = "ok"
	if (total < min) { status = "FAIL"; fail = 1 }
	printf "%-6s %6.1f%%  total\n", status, total
	if (fail) {
		printf "coverage gate: below %s%%\n", min > "/dev/stderr"
		exit 1
	}
}' "$profile"
