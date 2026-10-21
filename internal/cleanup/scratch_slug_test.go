//go:build !windows

package cleanup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestLongPathScratchAttribution(t *testing.T) {
	e, _, _, _ := fixture(t)
	ended := e.Now.Add(-48 * time.Hour)
	session := model.Session{Label: "finished", EndedAt: &ended, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "long-path", Cwd: "/" + strings.Repeat("a", 249)}}
	// Use the independently pinned slug, never projectSlug to create the fixture.
	path := filepath.Join(e.TempRoots[0], fmt.Sprintf("claude-%d", os.Getuid()), "-"+strings.Repeat("a", 199)+"-wkpu26", session.ExternalID)
	put(t, filepath.Join(path, "scratch"), "temporary fixture")
	cs := scratchCandidates(context.Background(), e, snapshot{sessions: []model.Session{session}})
	if len(cs) != 1 || cs[0].Path != path || cs[0].Status != "ready" {
		t.Fatalf("long path not attributed: %+v", cs)
	}
	if _, err := inspectScratch(context.Background(), e, snapshot{sessions: []model.Session{session}}, cs[0]); err != nil {
		t.Fatal(err)
	}
}
