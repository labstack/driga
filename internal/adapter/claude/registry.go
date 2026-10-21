package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/labstack/driga/internal/pathx"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/model"
)

type registryEntry struct {
	SessionID       string          `json:"sessionId"`
	Cwd             string          `json:"cwd"`
	Name            string          `json:"name"`
	Kind            string          `json:"kind"`
	Entrypoint      string          `json:"entrypoint"`
	Version         string          `json:"version"`
	StartedAt       json.RawMessage `json:"startedAt"`
	Status          string          `json:"status"`
	StatusUpdatedAt json.RawMessage `json:"statusUpdatedAt"`
}

func readRegistry(path string, env adapter.Env) (model.SessionObs, error) {
	pid, err := strconv.ParseInt(strings.TrimSuffix(filepath.Base(path), ".json"), 10, 32)
	if err != nil || pid <= 0 {
		return model.SessionObs{}, fmt.Errorf("invalid registry pid")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return model.SessionObs{}, err
	}
	var r registryEntry
	if err := json.Unmarshal(b, &r); err != nil {
		return model.SessionObs{}, err
	}
	if r.SessionID == "" {
		return model.SessionObs{}, fmt.Errorf("missing sessionId")
	}
	start, err := registryTime(r.StartedAt)
	if err != nil {
		return model.SessionObs{}, fmt.Errorf("startedAt: %w", err)
	}
	if len(r.StatusUpdatedAt) != 0 && !bytes.Equal(r.StatusUpdatedAt, []byte("null")) {
		if _, err := registryTime(r.StatusUpdatedAt); err != nil {
			return model.SessionObs{}, fmt.Errorf("statusUpdatedAt: %w", err)
		}
	}
	o := model.SessionObs{
		Adapter: "claude", ExternalID: r.SessionID, Cwd: r.Cwd,
		Version: r.Version, RegistryStatus: r.Status,
		ProcStart: start, StartedAt: start, Origin: registryOrigin(r),
	}
	if env.Alive != nil && env.Alive(int32(pid), start) {
		o.PID = int32(pid)
	}
	return o, nil
}

func registryTime(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return time.Time{}, fmt.Errorf("missing timestamp")
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return time.Time{}, err
		}
		return time.Parse(time.RFC3339Nano, s)
	}
	var ms int64
	if err := json.Unmarshal(raw, &ms); err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(ms), nil
}

func registryOrigin(r registryEntry) string {
	if strings.Contains(r.Entrypoint, "sdk") || r.Kind == "headless" || r.Kind == "print" {
		return "headless"
	}
	return "interactive"
}

func temporaryCwd(cwd string) bool {
	if cwd == "" {
		return false
	}
	cwd = canonicalPath(cwd)
	for _, root := range pathx.TempRoots() {
		if pathx.Within(cwd, canonicalPath(root)) {
			return true
		}
	}
	return false
}

func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	// A logged cwd may already be gone. Resolve its existing ancestors
	// while retaining the missing suffix as a clean path.
	path = filepath.Clean(path)
	if parent := filepath.Dir(path); parent != path {
		return filepath.Join(canonicalPath(parent), filepath.Base(path))
	}
	return path
}
