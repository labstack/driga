package cleanup

import (
	"os"
	"path/filepath"
)

func historyCandidates(e Env) []Candidate {
	cs := []Candidate{}
	for _, h := range []struct{ path, setting string }{{".claude/projects", "Claude Code cleanupPeriodDays"}, {".codex/sessions", "Codex retention settings"}, {".gemini/tmp", "Gemini retention settings"}, {".gemini/antigravity-cli/conversations", "agy retention settings"}} {
		p := filepath.Join(e.Home, h.path)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			cs = append(cs, Candidate{ID: "history:" + h.path, Kind: History, Path: p, Status: "report_only", Evidence: []string{"agent-owned history; use " + h.setting}, Ignored: []IgnoredPath{}})
		}
	}
	return cs
}
