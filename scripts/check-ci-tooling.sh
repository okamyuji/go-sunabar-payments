#!/usr/bin/env bash
set -euo pipefail

workflow="${1:-.github/workflows/ci.yml}"

if [ ! -f "$workflow" ]; then
	echo "workflow file not found: $workflow" >&2
	exit 1
fi

action_ref="$(
	awk '
		/uses:[[:space:]]*golangci\/golangci-lint-action@/ {
			sub(/^.*golangci\/golangci-lint-action@/, "")
			sub(/[[:space:]]*$/, "")
			print
			exit
		}
	' "$workflow"
)"
lint_version="$(
	awk '
		/uses:[[:space:]]*golangci\/golangci-lint-action@/ { in_step = 1; next }
		in_step && /^[[:space:]]*-[[:space:]]+(name|uses|run):/ { in_step = 0 }
		in_step && /^[[:space:]]*version:[[:space:]]*/ { print $2; exit }
	' "$workflow"
)"

if [ -z "$action_ref" ]; then
	echo "golangci-lint action is not configured in $workflow" >&2
	exit 1
fi

if [ -z "$lint_version" ]; then
	echo "golangci-lint action version is not pinned in $workflow" >&2
	exit 1
fi

action_major="${action_ref#v}"
action_major="${action_major%%.*}"
lint_semver="${lint_version#v}"
lint_major="${lint_semver%%.*}"
lint_rest="${lint_semver#*.}"
lint_minor="${lint_rest%%.*}"

if [[ ! "$action_major" =~ ^[0-9]+$ || ! "$lint_major" =~ ^[0-9]+$ || ! "$lint_minor" =~ ^[0-9]+$ ]]; then
	echo "unable to parse golangci-lint action/version: action=$action_ref version=$lint_version" >&2
	exit 1
fi

if [ "$lint_major" -ge 2 ] && [ "$action_major" -lt 7 ]; then
	echo "golangci-lint $lint_version requires golangci/golangci-lint-action v7 or later, found $action_ref" >&2
	exit 1
fi

if [ "$lint_major" -eq 2 ] && [ "$lint_minor" -ge 1 ] && [ "$action_major" -lt 8 ]; then
	echo "golangci-lint $lint_version requires golangci/golangci-lint-action v8 or later, found $action_ref" >&2
	exit 1
fi

echo "golangci-lint action/version compatibility OK ($action_ref, $lint_version)"
