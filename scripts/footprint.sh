#!/usr/bin/env bash
# Maintainer-run acceptance check. Reads real agent histories; isolates Driga state.
set -euo pipefail

if [[ $(uname -s) != Darwin ]]; then
	printf '%s\n' 'footprint.sh requires macOS (ps RSS and top IDLEW).' >&2
	exit 2
fi

repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/driga-footprint.XXXXXX")
umask 077
collector_pid=''
started_pids=()
stop_started() {
	local pid
	for pid in "${started_pids[@]}"; do
		kill -TERM "$pid" 2>/dev/null || true
	done
	for pid in "${started_pids[@]}"; do
		wait "$pid" 2>/dev/null || true
	done
	started_pids=()
}
cleanup() {
	stop_started
	rm -rf -- "$scratch"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

export GOCACHE=${GOCACHE:-"$scratch/gocache"}
export GOPROXY=off
export CGO_ENABLED=0
cd -- "$repo"
go build -o "$scratch/driga" ./cmd/driga

# Capture effective collection settings read-only. Fleet and intake are set
# explicitly in each isolated config, so drop the user's sections for them.
"$scratch/driga" config show | awk '
	/^\[/ { skip = ($0 == "[fleet]" || $0 == "[intake]") }
	!skip' >"$scratch/collection.toml"
for node in collector viewer1 viewer2; do
	mkdir -p "$scratch/$node/data" "$scratch/$node/spool" "$scratch/$node/home"
done
cp "$scratch/collection.toml" "$scratch/collector/config.toml"
cat >>"$scratch/collector/config.toml" <<'CONFIG'
[fleet]
serve = false
[intake]
otlp = false
CONFIG
for node in viewer1 viewer2; do
	cat >"$scratch/$node/config.toml" <<'CONFIG'
[adapters.claude]
enabled = false
[adapters.codex]
enabled = false
[fleet]
serve = false
[intake]
otlp = false
CONFIG
done
# Exec is essential: $! must be Driga, not a shell with an orphanable child.
# Collector reads real histories; viewers have empty HOME/CODEX_HOME directories.
cat >"$scratch/run" <<'RUN'
#!/usr/bin/env bash
set -euo pipefail
root=$(cd -- "$(dirname -- "$0")" && pwd)
node=$1
shift
export DRIGA_DATA_DIR="$root/$node/data"
export DRIGA_SPOOL_DIR="$root/$node/spool"
export DRIGA_CONFIG="$root/$node/config.toml"
if [[ $node != collector ]]; then
	export HOME="$root/$node/home"
	export CODEX_HOME="$HOME/.codex"
fi
exec "$root/driga" "$@"
RUN
chmod 700 "$scratch/run"
export DRIGA_DATA_DIR="$scratch/collector/data"
printf '%s\n' 'Reading real HOME and configured roots; all Driga state/config is isolated.' \
	'Phases: 5 min idle, 5 min with heartbeat, 10 min serving 2 loopback viewers.'
"$scratch/run" collector collect >"$scratch/collector.log" 2>&1 &
collector_pid=$!
started_pids+=("$collector_pid")

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
	ps -axo pid=,ppid=,rss= | awk -v root="${1:-$collector_pid}" '
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

# Physical footprint is what Activity Monitor shows. Unlike RSS it excludes
# memory the Go runtime has already returned to macOS (MADV_FREE_REUSABLE),
# which RSS keeps counting until the system reclaims it. Reported, not gated.
# peak_footprint prints a process's lifetime peak physical footprint in bytes.
peak_footprint() {
	vmmap -summary "$1" 2>/dev/null | awk -F: '/^Physical footprint \(peak\)/ {
		v = $2; gsub(/ /, "", v); n = v + 0; u = substr(v, length(v))
		if (u == "K") n *= 1024; else if (u == "M") n *= 1048576; else if (u == "G") n *= 1073741824
		printf "%d\n", n }'
}

sample_footprint() {
	local pid=${2:-$collector_pid} lines
	lines=$(vmmap -summary "$pid" 2>/dev/null | awk -F: '/^Physical footprint/ { gsub(/^ +/, "", $2); printf "%s=%s ", $1, $2 }') || lines=''
	printf '%s pid=%s: %s\n' "$1" "$pid" "${lines:-physical footprint unavailable}"
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
		sample_footprint idle
		: >"$scratch/idle-wakeups"
	elif [[ $phase == viewer && $SECONDS -ge $((started + 590)) && ! -e $scratch/viewer-wakeups ]]; then
		sample_wakeups
		sample_footprint viewer
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
# Restart only our collector with an explicit loopback listener; retain its
# representative history while measuring fleet overhead on the same workload.
# Keep the first collector's lifetime footprint peak before it exits.
first_peak=$(peak_footprint "$collector_pid")
stop_started
cp "$scratch/collection.toml" "$scratch/collector/config.toml"
cat >>"$scratch/collector/config.toml" <<'CONFIG'
[fleet]
serve = true
listen = "127.0.0.1:0"
[intake]
otlp = false
CONFIG
"$scratch/run" collector serve >"$scratch/collector.log" 2>&1 &
collector_pid=$!
started_pids+=("$collector_pid")
ready_by=$((SECONDS + 30))
until "$scratch/run" collector fleet status >/dev/null 2>&1; do
	if ! kill -0 "$collector_pid" 2>/dev/null || ((SECONDS >= ready_by)); then
		cat "$scratch/collector.log" >&2
		printf '%s\n' 'Fleet listener did not become ready.' >&2
		exit 1
	fi
	sleep 0.2
done
addr=$(cat "$DRIGA_DATA_DIR/fleet/listen")
[[ $addr == 127.0.0.1:* ]] || {
	printf 'Unexpected listener: %s\n' "$addr" >&2
	exit 1
}
peer_id=$("$scratch/run" collector fleet id)
for node in viewer1 viewer2; do
	viewer_id=$("$scratch/run" "$node" fleet id)
	"$scratch/run" collector fleet enroll --allow "$viewer_id"
	"$scratch/run" "$node" fleet add "$addr" --name footprint-peer --id "$peer_id"
	"$scratch/run" "$node" collect >"$scratch/$node.log" 2>&1 &
	started_pids+=("$!")
done
fleet_ready() {
	"$scratch/run" collector fleet status | awk '$2=="streams" && $1==2 && $4==2 {ok=1} END {exit !ok}'
}
ready_by=$((SECONDS + 30))
until fleet_ready; do
	if ((SECONDS >= ready_by)); then
		printf '%s\n' 'Two watching fleet streams did not become ready.' >&2
		cat "$scratch/collector.log" "$scratch/viewer1.log" "$scratch/viewer2.log" >&2
		exit 1
	fi
	touch "$scratch/viewer1/data/viewer.heartbeat" "$scratch/viewer2/data/viewer.heartbeat"
	sleep 1
done
phase=fleet
started=$SECONDS
fleet_peaks=(0 0 0)
fleet_tree_peaks=(0 0 0)
while ((SECONDS - started < 600)); do
	for i in 0 1 2; do
		pid=${started_pids[$i]}
		if ! kill -0 "$pid" 2>/dev/null; then
			printf 'Fleet process %s exited early.\n' "$pid" >&2
			cat "$scratch/collector.log" "$scratch/viewer1.log" "$scratch/viewer2.log" >&2
			exit 1
		fi
		read -r collector_rss tree_rss < <(sample_rss "$pid")
		children_rss=$((tree_rss - collector_rss))
		((collector_rss > fleet_peaks[i])) && fleet_peaks[i]=$collector_rss
		((tree_rss > fleet_tree_peaks[i])) && fleet_tree_peaks[i]=$tree_rss
		((collector_rss > peak_collector)) && peak_collector=$collector_rss
		((tree_rss > peak_tree)) && peak_tree=$tree_rss
		((children_rss > peak_children)) && peak_children=$children_rss
		printf '%3ss fleet pid=%s collector=%s KiB children=%s KiB total=%s KiB\n' "$((SECONDS - started))" "$pid" "$collector_rss" "$children_rss" "$tree_rss"
	done
	# Fail if pairing/streaming is lost: disconnected viewers are not a valid sample.
	if ! fleet_ready; then
		printf '%s\n' 'Fleet phase lost a watching stream.' >&2
		exit 1
	fi
	if ((SECONDS - started >= 590)) && [[ ! -e $scratch/fleet-wakeups ]]; then
		sample_wakeups
		for i in 0 1 2; do
			sample_footprint fleet "${started_pids[$i]}"
		done
		: >"$scratch/fleet-wakeups"
	fi
	touch "$scratch/viewer1/data/viewer.heartbeat" "$scratch/viewer2/data/viewer.heartbeat"
	sleep 2
	touch "$scratch/viewer1/data/viewer.heartbeat" "$scratch/viewer2/data/viewer.heartbeat"
	sleep 3
done
for i in 0 1 2; do
	printf 'fleet phase peak pid=%s: collector %s KiB; collector + descendants %s KiB\n' "${started_pids[$i]}" "${fleet_peaks[$i]}" "${fleet_tree_peaks[$i]}"
done
printf 'Sampled peak RSS: collector %s KiB; children %s KiB; combined %s KiB\n' "$peak_collector" "$peak_children" "$peak_tree"
printf '%s\n' '5-second sampling can miss short-lived subprocess peaks; RSS includes shared pages.'
# The budget is physical footprint, the memory macOS charges to the process;
# RSS above also counts clean code pages and is reported for comparison only.
if [[ -z $first_peak ]]; then
	printf '%s\n' 'FAIL: physical footprint unavailable for the first collector.' >&2
	exit 1
fi
peak_owned=$first_peak
for pid in "${started_pids[@]}"; do
	peak=$(peak_footprint "$pid")
	if [[ -z $peak ]]; then
		printf 'FAIL: physical footprint unavailable for pid %s.\n' "$pid" >&2
		exit 1
	fi
	((peak > peak_owned)) && peak_owned=$peak
done
printf 'Peak physical footprint: collector %s KiB\n' "$((peak_owned / 1024))"
if ((peak_owned > 50000000)); then
	printf '%s\n' 'FAIL: peak collector physical footprint exceeds 50 MB.' >&2
	exit 1
fi
printf '%s\n' 'PASS: peak collector physical footprint <= 50 MB (review combined RSS separately).'
