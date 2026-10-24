#!/usr/bin/env bash
# Re-records the website footage from the real binary: builds driga and
# driga-demo, films the TUI with VHS and the web dashboard with Playwright in
# both themes, and cuts the films with Remotion into out/media (posters into
# src/assets/media). `just site-footage-publish` then uploads them to R2.
# Needs vhs and ffmpeg on PATH.
set -euo pipefail
cd "$(dirname "$0")"
work=$(mktemp -d)
cleanup() {
  pkill -f "driga-demo --data-dir $work" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT
(cd ../.. && go build -o "$work/driga" ./cmd/driga && go build -o "$work/driga-demo" ./cmd/driga-demo)
npm ci --no-audit --no-fund
npx playwright install chromium
export FOOTAGE_DRIGA="$work/driga" FOOTAGE_DEMO="$work/driga-demo"
mkdir -p out
# Each theme twice: the full width for the film, 1440 wide for the Machines loop.
for theme in dark light; do
  for take in flow machines; do
    out="out/tui-$theme.mp4" width=1920
    [[ $take == machines ]] && out="out/tui-machines-$theme.mp4" width=1440
    sed -e "s#out/tui-dark.mp4#$out#" -e "s/^Set Width 1920/Set Width $width/" flow.tape > "$work/$take-$theme.tape"
    [[ $theme == light ]] && sed -i.bak -e 's/Catppuccin Mocha/Catppuccin Latte/' -e 's/#11111b/#dce0e8/' "$work/$take-$theme.tape"
    FOOTAGE_DIR="$work/tui-$take-$theme" FOOTAGE_UI=$theme vhs "$work/$take-$theme.tape"
  done
done
FOOTAGE_DIR="$work/web" node web.mjs
node render.mjs
