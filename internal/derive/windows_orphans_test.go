package derive

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

func TestWindowsParentIdentity(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name   string
		parent *model.Proc
		orphan bool
	}{
		{"gone", nil, true},
		{"reused", &model.Proc{PID: 700, StartTime: now}, true},
		{"live", &model.Proc{PID: 700, StartTime: now.Add(-time.Hour)}, false},
		{"unknown", &model.Proc{PID: 700}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := []model.Proc{{PID: 701, PPID: 700, StartTime: now.Add(-time.Minute), Cmdline: "claude mcp serve", Argv: []string{"claude", "mcp", "serve"}}}
			if tc.parent != nil {
				parent := *tc.parent
				parent.PPID = 600
				ps = append(ps, parent, model.Proc{PID: 600, PPID: 600, StartTime: now.Add(-2 * time.Hour)})
			}
			idx := newProcIndex(ps, true)
			got := FindOrphans(idx, nil, signatures.Builtin(), now, 0, nil, WithExternalPeers(map[int32]model.PeerState{701: model.PeerNone}), WithSubreaper(700))
			if (len(got) == 1) != tc.orphan {
				t.Fatalf("orphans: %+v", got)
			}
			if tc.orphan && !strings.Contains(strings.Join(got[0].Evidence, ";"), "parent") {
				t.Fatal(got[0].Evidence)
			}
			if tc.name == "reused" && (len(idx.Ancestors(701)) != 0 || len(idx.Descendants(700)) != 0) {
				t.Fatal("reused parent linked")
			}
		})
	}
}

func TestWindowsOrphanPinsEveryMember(t *testing.T) {
	now := time.Now()
	ps := []model.Proc{{PID: 701, PPID: 700, StartTime: now.Add(-time.Hour), Cmdline: `"C:\Tools\claude.exe" mcp serve`, Argv: []string{"C:\\Tools\\claude.exe", "mcp", "serve"}}, {PID: 702, PPID: 701, StartTime: now.Add(-time.Minute)}}
	peers := WithExternalPeers(map[int32]model.PeerState{701: model.PeerNone, 702: model.PeerNone})
	first := FindOrphans(newProcIndex(ps, true), nil, signatures.Builtin(), now, 0, nil, peers)
	if len(first) != 1 || len(first[0].ProcessStarts) != 2 || first[0].Verified {
		t.Fatalf("first: %+v", first)
	}
	prev := map[string]model.Orphan{first[0].ID: first[0]}
	second := FindOrphans(newProcIndex(ps, true), nil, signatures.Builtin(), now, 0, prev, peers)
	if len(second) != 1 || !second[0].Verified {
		t.Fatalf("second: %+v", second)
	}
	ps[1].StartTime = ps[1].StartTime.Add(time.Second)
	reused := FindOrphans(newProcIndex(ps, true), nil, signatures.Builtin(), now, 0, prev, peers)
	if len(reused) != 1 || reused[0].Verified {
		t.Fatalf("reused child verified: %+v", reused)
	}
}
