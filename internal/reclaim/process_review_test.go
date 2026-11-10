package reclaim

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

var reviewNow = time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

func reviewProc(pid, parent int32, exe string, args ...string) model.Proc {
	return model.Proc{PID: pid, PPID: parent, StartTime: reviewNow.Add(-time.Hour), Exe: exe, Argv: append([]string{exe}, args...), Cmdline: strings.Join(append([]string{exe}, args...), " "), Cwd: "/work/project", OwnerKnown: true, OwnerUID: 501, OwnerSID: "S-1-5-21-123", RSS: 10}
}

func reviewInput(ps ...model.Proc) ProcessReviewInput {
	return ProcessReviewInput{Processes: ps, SourceOS: "linux", Now: reviewNow, MinAge: 10 * time.Minute, Self: model.Proc{PID: 9000, OwnerKnown: true, OwnerUID: 501, OwnerSID: "S-1-5-21-123"}}
}

func TestProcessReviewAddsGenericDetachedWork(t *testing.T) {
	p := reviewProc(50, 1, "/usr/bin/python3", "/tmp/run-check.py")
	if got := Opportunities(nil, nil, nil, derive.NewProcIndex([]model.Proc{p})); len(got) != 0 {
		t.Fatalf("precondition: existing helper/session opportunities changed: %v", got)
	}
	got := ReviewProcesses(reviewInput(p))
	if len(got) != 1 || got[0].Category != "script" || got[0].Name != "run-check.py" || got[0].Status != "review" || got[0].Cwd != p.Cwd {
		t.Fatalf("detached generic script missing: %+v", got)
	}
}

func TestProcessReviewCategoriesAndProtections(t *testing.T) {
	tests := []struct {
		name             string
		p                model.Proc
		category, reason string
		omitted          bool
	}{
		{"tiny helper", func() model.Proc { p := reviewProc(51, 1, "/opt/bin/broker"); p.Role = "helper"; return p }(), "agent_helper", "socket observation unavailable", false},
		{"dev server", reviewProc(52, 1, "/usr/bin/node", "/work/project/server.js"), "dev_server", "socket observation unavailable", false},
		{"temporary database", reviewProc(53, 1, "/usr/bin/postgres", "-D", "/tmp/test-cluster"), "test_database", "temporary", false},
		{"main database", reviewProc(54, 1, "/usr/bin/postgres", "-D", "/var/lib/postgresql/data"), "database", "managed", true},
		{"compose", reviewProc(55, 1, "/usr/bin/process-compose"), "managed", "managed", true},
		{"adb", reviewProc(56, 1, "/usr/bin/adb", "fork-server", "server"), "managed", "managed", true},
		{"system", reviewProc(57, 1, "/usr/lib/systemd/systemd"), "managed", "managed", true},
		{"desktop", reviewProc(58, 1, "/Applications/Editor.app/Contents/MacOS/Editor"), "managed", "managed", true},
		{"zombie", func() model.Proc { p := reviewProc(59, 1, "python3", "/tmp/job.py"); p.Zombie = true; return p }(), "script", "zombie", true},
		{"foreign owner", func() model.Proc { p := reviewProc(60, 1, "python3", "/tmp/job.py"); p.OwnerUID = 0; return p }(), "script", "different user", true},
		{"unknown owner", func() model.Proc { p := reviewProc(61, 1, "python3", "/tmp/job.py"); p.OwnerKnown = false; return p }(), "script", "owner unavailable", true},
		{"unknown identity", func() model.Proc { p := reviewProc(62, 1, ""); p.Argv = nil; p.Cmdline = ""; return p }(), "unknown", "identity incomplete", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := reviewInput(tt.p)
			got := ReviewProcesses(in)
			if tt.omitted && len(got) != 0 {
				t.Fatalf("protected daemon included by default: %+v", got)
			}
			in.TargetPID = tt.p.PID
			got = ReviewProcesses(in)
			if len(got) != 1 || got[0].Category != tt.category || !strings.Contains(strings.Join(got[0].Reasons, ";"), tt.reason) {
				t.Fatalf("unexpected explicit review: %+v", got)
			}
			if tt.omitted && got[0].Status != "protected" {
				t.Fatalf("unsafe explicit status: %+v", got[0])
			}
		})
	}
}

func TestProcessReviewTreeAndObservationAvailability(t *testing.T) {
	root := reviewProc(70, 1, "python3", "/tmp/job.py")
	child := reviewProc(71, 70, "python3", "/tmp/worker.py")
	in := reviewInput(child, root)
	in.CPUKnown = map[int32]bool{70: true}
	in.Peers = map[int32]model.PeerState{70: model.PeerNone}
	root.CPUPercent = 2
	in.Processes[1] = root
	got := ReviewProcesses(in)
	if len(got) != 1 || len(got[0].PIDs) != 2 || got[0].Memory.Memory != 20 || got[0].CPUKnown || got[0].PeersKnown {
		t.Fatalf("tree/evidence incorrectly aggregated: %+v", got)
	}
	in.CPUKnown[71] = true
	in.Peers[71] = model.PeerListening
	got = ReviewProcesses(in)
	if !got[0].CPUKnown || !got[0].PeersKnown || got[0].Status != "protected" || got[0].CPU != 2 {
		t.Fatalf("listener not protected: %+v", got)
	}
	in.Peers[71] = model.PeerConnected
	got = ReviewProcesses(in)
	if !strings.Contains(strings.Join(got[0].Reasons, ";"), "connected") {
		t.Fatalf("connection omitted: %+v", got)
	}
}

func TestProcessReviewLiveSessionDirectoryAndRememberedOwnership(t *testing.T) {
	shell := reviewProc(80, 1, "python3", "/tmp/check.py")
	shell.Cwd = "/work/project/nested"
	sessionProc := reviewProc(81, 2, "codex")
	in := reviewInput(shell, sessionProc)
	in.Sessions = []model.Session{{ID: 7, SessionObs: model.SessionObs{PID: 81, ProcStart: sessionProc.StartTime, Cwd: "/work/project"}}}
	got := ReviewProcesses(in)
	if len(got) != 1 || got[0].Status != "protected" || !strings.Contains(strings.Join(got[0].Reasons, ";"), "overlapping directory") {
		t.Fatalf("live nested directory lost: %+v", got)
	}
	in.Processes[0].Cwd = "/other"
	in.Orphans = []view.OrphanRow{{Orphan: model.Orphan{RootPID: 80, StartTime: shell.StartTime, Evidence: []string{"spawned by session 7"}}}}
	got = ReviewProcesses(in)
	if len(got) != 1 || got[0].Status != "protected" || !strings.Contains(strings.Join(got[0].Reasons, ";"), "remembered") {
		t.Fatalf("remembered ownership lost: %+v", got)
	}
}

func TestProcessReviewWindowsReusedParentAndDirectoryVolumes(t *testing.T) {
	p := reviewProc(90, 91, `C:\Python\python.exe`, `C:\Temp\check.py`)
	p.Cwd = `C:\work\project-two`
	parent := reviewProc(91, 2, `C:\Windows\host.exe`)
	parent.StartTime = reviewNow.Add(-time.Minute)
	in := reviewInput(p, parent)
	in.SourceOS = "windows"
	in.Sessions = []model.Session{{ID: 8, SessionObs: model.SessionObs{Hosted: true, Cwd: `C:\work\project`}}}
	got := ReviewProcesses(in)
	if len(got) != 1 || got[0].RootPID != 90 || got[0].Status != "review" {
		t.Fatalf("reused parent/sibling path: %+v", got)
	}
	in.Processes[0].Cwd = `D:\work\project`
	got = ReviewProcesses(in)
	if len(got) != 1 || got[0].Status != "review" {
		t.Fatalf("volume incorrectly overlaps: %+v", got)
	}
}

func TestProcessReviewExplicitAttachedYoungMissingStartAndMissingPID(t *testing.T) {
	p := reviewProc(100, 101, "python3", "/tmp/check.py")
	parent := reviewProc(101, 1, "shell")
	p.StartTime = reviewNow.Add(-time.Second)
	in := reviewInput(p, parent)
	in.TargetPID = 100
	got := ReviewProcesses(in)
	if len(got) != 1 || got[0].Status != "protected" || !strings.Contains(strings.Join(got[0].Reasons, ";"), "attached") || !strings.Contains(strings.Join(got[0].Reasons, ";"), "young") {
		t.Fatalf("explicit attached young: %+v", got)
	}
	in.Processes[0].StartTime = time.Time{}
	got = ReviewProcesses(in)
	if len(got) != 1 || !strings.Contains(strings.Join(got[0].Reasons, ";"), "start time unavailable") {
		t.Fatalf("missing start: %+v", got)
	}
	in.TargetPID = 999
	if got := ReviewProcesses(in); len(got) != 0 {
		t.Fatalf("missing pid fabricated: %+v", got)
	}
}

func TestProcessReviewInitAdoptionSignaturesAndInlinePrivacy(t *testing.T) {
	init := reviewProc(1, 0, "/sbin/init")
	script := reviewProc(110, 1, "/usr/bin/python3", "/tmp/check.py")
	inline := reviewProc(111, 1, "/usr/bin/python3", "-c", "private inline payload.py")
	host := reviewProc(112, 1, "/opt/bin/codex", "app-server")
	in := reviewInput(init, script, inline, host)
	in.CPUKnown = map[int32]bool{110: true, 111: true, 112: true}
	in.Peers = map[int32]model.PeerState{110: model.PeerNone, 111: model.PeerNone, 112: model.PeerNone}
	got := ReviewProcesses(in)
	if len(got) != 3 {
		t.Fatalf("expected detached roots only: %+v", got)
	}
	for _, r := range got {
		if r.Status != "review" || strings.Contains(r.Name, "private") {
			t.Fatalf("init incorrectly protects adopted work or inline payload disclosed: %+v", r)
		}
		if r.RootPID == 112 && r.Category != "agent_helper" {
			t.Fatalf("embedded host signature ignored: %+v", r)
		}
	}
	if in.Processes[1].Argv[1] != "/tmp/check.py" || len(in.Processes) != 4 {
		t.Fatal("input mutated")
	}
}

func TestProcessReviewKnownBrokerClientlessSocketsRemainReviewOnly(t *testing.T) {
	broker := reviewProc(120, 1, "/usr/bin/node", "/tmp/app-server-broker.mjs")
	server := reviewProc(121, 1, "/usr/bin/node", "/work/project/server.js")
	for _, state := range []model.PeerState{model.PeerListening, model.PeerSystem} {
		in := reviewInput(broker, server)
		in.Peers = map[int32]model.PeerState{120: state, 121: state}
		in.CPUKnown = map[int32]bool{120: true, 121: true}
		got := ReviewProcesses(in)
		if len(got) != 2 || got[0].Status != "review" || got[1].Status != "protected" || !got[0].IdentityKnown {
			t.Fatalf("shared broker socket policy not honored for state %d: %+v", state, got)
		}
		in.Peers[120] = model.PeerConnected
		got = ReviewProcesses(in)
		if got[0].Status != "protected" {
			t.Fatalf("connected broker must remain protected: %+v", got[0])
		}
		delete(in.Peers, 120)
		in.Processes[0].Exe = ""
		got = ReviewProcesses(in)
		if got[0].PeersKnown || got[0].IdentityKnown || got[0].Status == "ready" {
			t.Fatalf("missing evidence inferred as complete: %+v", got[0])
		}
	}
}

func TestProcessReviewNativeAgentProtectsWorkWithoutSessionRecords(t *testing.T) {
	for _, mode := range [][]string{nil, {"exec", "check"}} {
		agent := reviewProc(130, 2, "/opt/bin/codex", mode...)
		agent.Cwd = "/work/project"
		script := reviewProc(131, 1, "python3", "/tmp/stage-check.py")
		script.Cwd = "/work/project/nested"
		child := reviewProc(132, 131, "sleep", "30")
		child.Cwd = script.Cwd
		sibling := reviewProc(133, 1, "python3", "/tmp/other.py")
		sibling.Cwd = "/work/project-two"
		in := reviewInput(agent, script, child, sibling)
		got := ReviewProcesses(in)
		byPID := map[int32]ProcessReview{}
		for _, r := range got {
			byPID[r.RootPID] = r
		}
		if byPID[131].Status != "protected" || !strings.Contains(strings.Join(byPID[131].Reasons, ";"), "live agent PID 130") || len(byPID[131].PIDs) != 2 || byPID[133].Status != "review" {
			t.Fatalf("native agent directory ownership lost (mode %v): %+v", mode, got)
		}
		in.TargetPID = 130
		got = ReviewProcesses(in)
		if len(got) != 1 || got[0].Status != "protected" || !strings.Contains(strings.Join(got[0].Reasons, ";"), "live agent PID 130") {
			t.Fatalf("explicit agent explanation missing: %+v", got)
		}
	}
}

func TestProcessReviewNativeAgentWindowsDirectoryIdentity(t *testing.T) {
	agent := reviewProc(140, 2, `C:\tools\codex.exe`)
	agent.Cwd = `C:\work\Project`
	script := reviewProc(141, 1, `C:\tools\python.exe`, `C:\Temp\stage.py`)
	script.Cwd = `c:\WORK\project\nested`
	sibling := reviewProc(142, 1, `C:\tools\python.exe`, `C:\Temp\other.py`)
	sibling.Cwd = `C:\work\Project-two`
	volume := reviewProc(143, 1, `C:\tools\python.exe`, `C:\Temp\other.py`)
	volume.Cwd = `D:\work\Project`
	in := reviewInput(agent, script, sibling, volume)
	in.SourceOS = "windows"
	got := ReviewProcesses(in)
	byPID := map[int32]ProcessReview{}
	for _, r := range got {
		byPID[r.RootPID] = r
	}
	if byPID[141].Status != "protected" || byPID[142].Status != "review" || byPID[143].Status != "review" {
		t.Fatalf("native Windows directory identity lost: %+v", got)
	}
}

func TestProcessReviewNativeAgentMissingDirectoryProtectsUnknownOwnership(t *testing.T) {
	agent := reviewProc(150, 2, "/opt/bin/codex")
	agent.Cwd = ""
	helper := reviewProc(151, 1, "/usr/bin/node", "/tmp/app-server-broker.mjs")
	helper.Cwd = "/work/separate"
	in := reviewInput(agent, helper)
	in.CPUKnown = map[int32]bool{150: true, 151: true}
	in.Peers = map[int32]model.PeerState{150: model.PeerNone, 151: model.PeerNone}
	in.TargetPID = 151
	got := ReviewProcesses(in)
	if len(got) != 1 || got[0].Status != "protected" || !got[0].IdentityKnown || !got[0].CPUKnown || !got[0].PeersKnown || !strings.Contains(strings.Join(got[0].Reasons, ";"), "live agent PID 150") || !strings.Contains(strings.Join(got[0].Reasons, ";"), "directory unavailable") {
		t.Fatalf("unreadable native agent directory hid ownership uncertainty: %+v", got)
	}
	// A known host/helper with an unreadable directory does not establish a live owner.
	in.Processes[0] = reviewProc(150, 2, "/opt/bin/codex", "app-server")
	in.Processes[0].Cwd = ""
	got = ReviewProcesses(in)
	if len(got) != 1 || got[0].Status != "review" {
		t.Fatalf("host alone incorrectly proves live ownership: %+v", got)
	}
	// Another user's agent is not an owner of this user's detached work.
	in.Processes[0] = agent
	in.Processes[0].OwnerUID = 502
	got = ReviewProcesses(in)
	if len(got) != 1 || got[0].Status != "review" {
		t.Fatalf("foreign agent incorrectly proves live ownership: %+v", got)
	}
}

func TestProcessReviewUnknownNativeAgentOwnerCannotHideOverlap(t *testing.T) {
	agent := reviewProc(210, 2, "/opt/bin/codex")
	agent.OwnerKnown = false
	helper := reviewProc(211, 1, "python3", "/tmp/check.py")
	in := reviewInput(agent, helper)
	in.TargetPID = helper.PID
	got := ReviewProcesses(in)
	if len(got) != 1 || got[0].Status != "protected" || !strings.Contains(strings.Join(got[0].Reasons, ";"), "process owner unavailable for live agent") {
		t.Fatalf("unknown native owner hid overlapping work: %+v", got)
	}
	in.Processes[0].Cwd += "-separate"
	got = ReviewProcesses(in)
	if len(got) != 1 || got[0].Status != "review" {
		t.Fatalf("disjoint native agent must not claim this tree: %+v", got)
	}
}

func TestProcessReviewJavaScriptDistinguishesScriptsFromServers(t *testing.T) {
	for _, tt := range []struct{ file, category string }{
		{"schedulers.mjs", "script"},
		{"client.ts", "script"},
		{"manager.js", "script"},
		{"app-server.ts", "dev_server"},
		{"astro.mjs", "dev_server"},
		{"vite", "dev_server"},
	} {
		p := reviewProc(160, 1, "/usr/bin/node", "/work/tools/"+tt.file)
		got := ReviewProcesses(reviewInput(p))
		if len(got) != 1 || got[0].Category != tt.category || got[0].Name != tt.file {
			t.Errorf("%s classification: %+v", tt.file, got)
		}
	}
}

func TestProcessReviewKnownCPUProtectsActiveWork(t *testing.T) {
	for _, manyChildren := range []bool{false, true} {
		root := reviewProc(170, 1, "python3", "/tmp/batch.py")
		idle := reviewProc(190, 1, "python3", "/tmp/idle.py")
		ps := []model.Proc{root, idle}
		known := map[int32]bool{170: true, 190: true}
		if manyChildren {
			for pid := int32(171); pid <= 180; pid++ {
				p := reviewProc(pid, 170, "python3", "/tmp/worker.py")
				p.CPUPercent = 3
				ps = append(ps, p)
				known[pid] = true
			}
		} else {
			ps[0].CPUPercent = derive.BackgroundCPUPercent
		}
		in := reviewInput(ps...)
		in.CPUKnown = known
		got := ReviewProcesses(in)
		if len(got) != 2 || got[0].RootPID != 170 || got[0].Status != "protected" || !strings.Contains(strings.Join(got[0].Reasons, ";"), "CPU indicates activity") || got[1].Status != "review" {
			t.Fatalf("active CPU unprotected (many children %v): %+v", manyChildren, got)
		}
	}
}

func TestProcessReviewMissingTreeDirectoryIsUnknownOwnership(t *testing.T) {
	agent := reviewProc(200, 2, "/opt/bin/codex")
	agent.Cwd = "/work/active"
	helper := reviewProc(201, 1, "/usr/bin/node", "/tmp/app-server-broker.mjs")
	helper.Cwd = ""
	in := reviewInput(agent, helper)
	in.CPUKnown = map[int32]bool{201: true}
	in.Peers = map[int32]model.PeerState{201: model.PeerNone}
	in.TargetPID = 201
	got := ReviewProcesses(in)
	if len(got) != 1 || got[0].Status != "protected" || got[0].OwnershipKnown || !got[0].IdentityKnown || !got[0].CPUKnown || !got[0].PeersKnown {
		t.Fatalf("unavailable directory implied known ownership: %+v", got)
	}
	// The missing observation remains unknown even before any agent is discovered.
	in.Processes = []model.Proc{helper}
	got = ReviewProcesses(in)
	if len(got) != 1 || got[0].Status != "protected" || got[0].OwnershipKnown {
		t.Fatalf("missing directory without agent inferred safe: %+v", got)
	}
	helper.Cwd = "/work/complete-root"
	child := reviewProc(202, 201, "sleep", "30")
	child.Cwd = ""
	in.Processes = []model.Proc{helper, child}
	in.CPUKnown[202] = true
	in.Peers[202] = model.PeerNone
	got = ReviewProcesses(in)
	if len(got) != 1 || got[0].Status != "protected" || got[0].OwnershipKnown || got[0].Cwd != helper.Cwd || !got[0].CPUKnown || !got[0].PeersKnown {
		t.Fatalf("missing descendant directory ignored or root provenance lost: %+v", got)
	}
}
