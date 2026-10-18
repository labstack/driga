#!/usr/bin/env bash
# Maintainer-run acceptance check. Reads real agent histories; isolates Driga state.
set -euo pipefail

if [[ $(uname -s) != Darwin ]]; then
	printf '%s\n' 'footprint.sh requires macOS (ps RSS and top IDLEW).' >&2
	exit 2
fi

repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/driga-footprint.XXXXXX")
collector_pid=''
cleanup() {
	if [[ -n $collector_pid ]]; then
		kill -TERM "$collector_pid" 2>/dev/null || true
		wait "$collector_pid" 2>/dev/null || true
	fi
	rm -rf -- "$scratch"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

export GOCACHE=${GOCACHE:-"$repo/.superpowers/gocache"}
export GOPROXY=off
export CGO_ENABLED=0
export DRIGA_DATA_DIR="$scratch/data"
export DRIGA_SPOOL_DIR="$scratch/spool"
mkdir -m 700 "$DRIGA_DATA_DIR" "$DRIGA_SPOOL_DIR"
cd -- "$repo"
go build -o "$scratch/driga" ./cmd/driga

printf '%s\n' 'Reading real HOME and configured roots for 10 minutes.' \
	'Driga DB/spool/heartbeat are isolated in a temporary directory.' \
	'Phases: 5 min without heartbeat, then 5 min with heartbeat.'
"$scratch/driga" collect >"$scratch/collector.log" 2>&1 &
collector_pid=$!

peak_collector=0
peak_tree=0
peak_children=0
phase_collector=0
phase_tree=0
started=$SECONDS
phase=idle

# ps reports KiB. Resolve descendants to a fixed point (row order is arbitrary),
# including grandchildren such as Git and lsof while they are present.
sample_rss() {
	ps -axo pid=,ppid=,rss= | awk -v root="$collector_pid" '
		{ parent[$1]=$2; rss[$1]=$3 }
		END {
			# Test membership with "in": reading owned[x] would create keys and
			# make the sum below cover unrelated processes.
			owned[root]=1
			do {
				changed=0
				for (pid in parent)
					if (!(pid in owned) && (parent[pid] in owned)) { owned[pid]=1; changed=1 }
			} while (changed)
			for (pid in owned) total+=rss[pid]
			printf "%d %d\n", rss[root], total
		}'
}

# top fields vary by macOS version. Keep its last IDLEW reading when supported;
# two samples provide a one-second observation window rather than boot totals.
sample_wakeups() {
	local value
	value=$(top -l 2 -s 1 -n 1 -pid "$collector_pid" -stats pid,idlew 2>/dev/null |
		awk -v pid="$collector_pid" '$1==pid && $2 ~ /^[0-9]+$/ { v=$2 } END { if (v!="") print v }') || value=''
	if [[ -n $value ]]; then
		printf '%s: top IDLEW=%s in its last 1-second sample (~%s/min if sustained)\n' "$phase" "$value" "$((value * 60))"
	else
		printf '%s: top IDLEW unavailable\n' "$phase"
	fi
}

while ((SECONDS - started < 600)); do
	if ! kill -0 "$collector_pid" 2>/dev/null; then
		printf '%s\n' 'Collector exited before measurement finished:' >&2
		cat "$scratch/collector.log" >&2
		exit 1
	fi
	if ((SECONDS - started >= 300)); then
		if [[ $phase == idle ]]; then
			printf 'idle phase peak: collector %s KiB; collector + descendants %s KiB\n' "$phase_collector" "$phase_tree"
			phase=viewer
			phase_collector=0
			phase_tree=0
		fi
		touch "$DRIGA_DATA_DIR/viewer.heartbeat"
	fi
	read -r collector_rss tree_rss < <(sample_rss)
	children_rss=$((tree_rss - collector_rss))
	((collector_rss > peak_collector)) && peak_collector=$collector_rss
	((tree_rss > peak_tree)) && peak_tree=$tree_rss
	((children_rss > peak_children)) && peak_children=$children_rss
	((collector_rss > phase_collector)) && phase_collector=$collector_rss
	((tree_rss > phase_tree)) && phase_tree=$tree_rss
	printf '%3ss %-6s collector=%s KiB children=%s KiB total=%s KiB\n' "$((SECONDS - started))" "$phase" "$collector_rss" "$children_rss" "$tree_rss"
	# Report one wakeup sample near the end of each phase.
	if [[ $phase == idle && $SECONDS -ge $((started + 290)) && ! -e $scratch/idle-wakeups ]]; then
		sample_wakeups
		: >"$scratch/idle-wakeups"
	elif [[ $phase == viewer && $SECONDS -ge $((started + 590)) && ! -e $scratch/viewer-wakeups ]]; then
		sample_wakeups
		: >"$scratch/viewer-wakeups"
	fi
	# Refresh twice between 5-second RSS samples: freshness requires age < 5s.
	if [[ $phase == viewer ]]; then
		sleep 2
		touch "$DRIGA_DATA_DIR/viewer.heartbeat"
		sleep 3
	else
		sleep 5
	fi
done

printf 'viewer phase peak: collector %s KiB; collector + descendants %s KiB\n' "$phase_collector" "$phase_tree"
printf 'Sampled peak RSS: collector %s KiB; children %s KiB; combined %s KiB\n' "$peak_collector" "$peak_children" "$peak_tree"
printf '%s\n' '5-second sampling can miss short-lived subprocess peaks; RSS includes shared pages.'
if ((peak_collector * 1024 > 50000000)); then
	printf '%s\n' 'FAIL: peak collector RSS exceeds 50 MB.' >&2
	exit 1
fi
printf '%s\n' 'PASS: sampled peak collector RSS <= 50 MB (review combined RSS separately).'
