package cleanup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestWindowsRecycleRefusesRedirectedParent(t *testing.T) {
	root := t.TempDir()
	target, link := filepath.Join(root, "target"), filepath.Join(root, "redirect")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("junction: %v: %s", err, out)
	}
	path := filepath.Join(link, "keep.txt")
	if err := os.WriteFile(path, []byte("must not be recycled"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = trash(context.Background(), Env{}, Candidate{Path: path, identity: info}, ApplyOpts{}, Result{})
	if err == nil || (!strings.Contains(err.Error(), "reparse point") && !strings.Contains(err.Error(), "not redirected")) {
		t.Fatalf("redirected parent accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "keep.txt")); err != nil {
		t.Fatal("refusal touched the target", err)
	}
}

func TestWindowsScratchRemainsRefused(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	_, err := inspectScratch(context.Background(), Env{}, snapshot{}, Candidate{})
	if err == nil {
		t.Fatal("unverified scratch accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("refusal touched data directory", err)
	}
	if got := scratchCandidates(context.Background(), Env{TempRoots: []string{dir}}, snapshot{}); len(got) != 0 {
		t.Fatal(got)
	}
}

type windowsCleanupPlatform struct{}

func (windowsCleanupPlatform) Procs(context.Context) ([]model.Proc, error) { return nil, nil }
func (windowsCleanupPlatform) Machine(context.Context) (model.MachineSample, error) {
	return model.MachineSample{}, nil
}
func (windowsCleanupPlatform) Cwds(context.Context, []int32) (map[int32]string, error) {
	return nil, nil
}
func (windowsCleanupPlatform) Alive(int32, time.Time) bool { return false }

func TestWindowsCleanupRecyclesVerifiedWorktreeAndLogs(t *testing.T) {
	dir := t.TempDir()
	for key, value := range map[string]string{"HOME": dir, "USERPROFILE": dir, "APPDATA": dir, "LOCALAPPDATA": dir, "DRIGA_DATA_DIR": filepath.Join(dir, "data"), "DRIGA_SPOOL_DIR": filepath.Join(dir, "spool"), "DRIGA_CONFIG": filepath.Join(dir, "config.toml")} {
		t.Setenv(key, value)
	}
	repo, work, index := filepath.Join(dir, "repo"), filepath.Join(dir, "work"), filepath.Join(dir, "index")
	for _, path := range []string{repo, work} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(index, nil, 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(index, old, old); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.Tx(ctx, func(tx *store.Tx) error {
		id, err := tx.UpsertRepo(model.Repo{Path: repo, Name: "test"})
		if err != nil {
			return err
		}
		_, err = tx.UpsertWorktree(model.Worktree{RepoID: id, Path: work})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var actions []string
	e := Env{Home: dir, DataDir: filepath.Join(dir, "data"), Store: st, Plat: windowsCleanupPlatform{}, Cfg: config.Default().Cleanup, Git: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch joined {
		case "worktree list --porcelain -z":
			return []byte("worktree " + repo + "\x00\x00worktree " + work + "\x00branch refs/heads/test-cleanup\x00\x00"), nil
		case "rev-parse HEAD", "rev-parse --verify refs/remotes/origin/main":
			return []byte("head"), nil
		case "symbolic-ref refs/remotes/origin/HEAD":
			return []byte("refs/remotes/origin/main"), nil
		case "status --porcelain=v1 -z --ignored=matching --untracked-files=all", "merge-base --is-ancestor head head":
			return nil, nil
		case "rev-parse --symbolic-full-name @{u}":
			return nil, fmt.Errorf("no upstream")
		case "log -1 --format=%ct":
			return []byte(fmt.Sprint(old.Unix())), nil
		case "rev-parse --git-path index":
			return []byte(index), nil
		case "worktree prune", "branch -d -- test-cleanup":
			actions = append(actions, joined)
			return nil, nil
		default:
			return nil, fmt.Errorf("unexpected test git command %q", joined)
		}
	}}
	cs, err := List(ctx, e)
	if err != nil || len(cs) != 1 {
		t.Fatalf("list: %+v %v", cs, err)
	}
	r, err := Apply(ctx, e, cs[0], ApplyOpts{DryRun: true})
	if err != nil || len(r.Commands) != 3 || r.Commands[0][0] != "IFileOperation" || len(actions) != 0 {
		t.Fatalf("dry: %+v %v %v", r, err, actions)
	}
	r, err = Apply(ctx, e, cs[0], ApplyOpts{})
	if err != nil || len(actions) != 2 {
		t.Fatalf("apply: %+v %v %v", r, err, actions)
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Fatal("worktree not recycled", err)
	}
	b, err := os.ReadFile(filepath.Join(e.DataDir, "cleanup.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatal("missing audit records")
	}
	var entry struct {
		Result Result `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil || entry.Result.TrashPath == "" {
		t.Fatal("missing recycle audit result", err)
	}
}

func TestWindowsRecycleOwnedFile(t *testing.T) {
	dir := t.TempDir()
	for key, value := range map[string]string{"HOME": dir, "USERPROFILE": dir, "APPDATA": dir, "LOCALAPPDATA": dir, "DRIGA_DATA_DIR": filepath.Join(dir, "data"), "DRIGA_SPOOL_DIR": filepath.Join(dir, "spool"), "DRIGA_CONFIG": filepath.Join(dir, "config.toml")} {
		t.Setenv(key, value)
	}
	path := filepath.Join(dir, "driga-owned-recycle-test 子.txt")
	if err := os.WriteFile(path, []byte("only a test-created file"), 0600); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	c := Candidate{Path: path, identity: info}
	r, err := trash(context.Background(), Env{}, c, ApplyOpts{DryRun: true}, Result{})
	if err != nil || r.TrashPath == "" {
		t.Fatalf("dry run: %+v %v", r, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("dry run touched file", err)
	}
	if _, err := trash(context.Background(), Env{}, c, ApplyOpts{}, Result{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("source not recycled", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("unrelated file touched", err)
	}
}
