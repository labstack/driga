package gemini

import (
	"context"
	"fmt"
	"io"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/tail"
)

func (a *geminiAdapter) readLegacy(ctx context.Context, path string, prev model.Offset) (conversation, model.Offset, []adapter.Warning, error) {
	if err := ctx.Err(); err != nil {
		return conversation{}, prev, nil, err
	}
	// Stat through tail's offset helper without scanning the JSON body for a
	// newline. Legacy objects can be one huge line, even when unchanged.
	snapshot, err := tail.Read(path, model.Offset{}, 0)
	if err != nil {
		return conversation{}, prev, nil, err
	}
	next := snapshot.Next
	next.Pos = next.Size
	if next.Size > maxBytes {
		delete(a.cache, path)
		return conversation{}, next, []adapter.Warning{{Kind: "skipped_backlog", Detail: fmt.Sprintf("%s: legacy JSON exceeds 8 MiB", path)}}, nil
	}
	if c, ok := a.cache[path]; ok && c.next.Inode == next.Inode && c.next.Size == next.Size && c.next.Mtime.Equal(next.Mtime) {
		return c.fold, next, nil, nil
	}
	f, err := a.open(path)
	if err != nil {
		return conversation{}, prev, nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return conversation{}, prev, nil, err
	}
	if int64(len(data)) > maxBytes {
		return conversation{}, next, []adapter.Warning{{Kind: "skipped_backlog", Detail: path}}, nil
	}
	var fold conversation
	if err := fold.metadata(data); err != nil {
		return conversation{}, next, []adapter.Warning{{Kind: "invalid_json", Detail: path}}, nil
	}
	a.remember(path, cachedFile{next: next, fold: fold})
	var warnings []adapter.Warning
	if fold.truncated {
		warnings = append(warnings, adapter.Warning{Kind: "skipped_backlog", Detail: path + ": fold retains the last 1024 messages"})
	}
	return fold, next, warnings, nil
}
