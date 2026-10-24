# One continuous, real-time take of the real `driga` terminal UI on
# driga-demo's scripted sessions: type driga, watch workers start, answer the
# permission prompt, look at Machines. Run through `just site-footage`;
# FOOTAGE_DEMO, FOOTAGE_DRIGA, FOOTAGE_DIR and FOOTAGE_UI come from it, and
# record.sh writes a light copy of this tape (theme, fill, output) and a
# narrower copy for the Machines loop.
Output out/tui-dark.mp4

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font Mono"
Set FontSize 24
Set Width 1920
Set Height 1080
Set Padding 28
Set Margin 44
Set MarginFill "#11111b"
Set WindowBar Colorful
Set WindowBarSize 44
Set BorderRadius 14
Set Framerate 30
Set Theme "Catppuccin Mocha"
Set TypingSpeed 90ms
Set CursorBlink false

Hide
Type `export HOME="$FOOTAGE_DIR/home" DRIGA_DATA_DIR="$FOOTAGE_DIR/data" DRIGA_SPOOL_DIR="$FOOTAGE_DIR/data/events" DRIGA_CONFIG="$FOOTAGE_DIR/data/config.toml" DRIGA_UI_THEME="$FOOTAGE_UI" COLORTERM=truecolor; mkdir -p "$HOME"; driga() { "$FOOTAGE_DRIGA" "$@"; }; PS1='\[\e[38;2;203;166;247m\]~/code/atlas\[\e[0m\] \[\e[38;2;166;227;161m\]main\[\e[0m\] $ '`
Enter
# The demo's clock starts here: workers at 3 s and 5 s, the prompt at 8 s.
# Without a live demo the viewer would snapshot this machine: wait for it,
# and give up rather than film a snapshot.
Type `("$FOOTAGE_DEMO" --data-dir "$DRIGA_DATA_DIR" >/dev/null 2>&1 &); for i in $(seq 100); do "$FOOTAGE_DRIGA" summary --json 2>/dev/null | grep -q '"mode": *"daemon"' && break; sleep 0.1; done; "$FOOTAGE_DRIGA" summary --json 2>/dev/null | grep -q '"mode": *"daemon"' && clear && echo "demo-$((6*7))-ready"`
Enter
Wait+Screen@15s /demo-42-ready/
Type "clear"
Enter
Sleep 300ms
Show

Sleep 700ms
Type "driga"
Sleep 400ms
Enter
Wait+Screen@15s /daemon/
Wait+Screen@30s /y Allow/
Sleep 2.5s
Tab
Sleep 1.2s
Type "y"
Sleep 3s
Type "2"
Sleep 3.5s
Type "0"
Sleep 2s

Hide
Type "q"
Sleep 300ms
Type `pkill -f "driga-demo --data-dir $DRIGA_DATA_DIR"`
Enter
