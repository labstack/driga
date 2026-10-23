package hooks

import "github.com/aymanbagabas/go-udiff"

// Unified renders a minimal line diff with three context lines and hunk ranges.
func (d Diff) Unified() string {
	if !d.Changed || d.Before == d.After {
		return ""
	}
	preview := udiff.Unified(d.Path, d.Path, d.Before, d.After)
	if d.FullRewrite {
		preview = "# Full settings rewrite: targeted edit unavailable\n" + preview
	}
	return preview
}
