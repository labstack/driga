package gemini

import (
	"context"
	"fmt"
	"os"
	recordpath "path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
)

const header = `{"sessionId":"synthetic-session","projectHash":"synthetic-project","kind":"main","startTime":"2026-10-18T12:00:00Z","lastUpdated":"2026-10-18T12:00:00Z"}` + "\n"
const user1 = `{"id":"u1","type":"user","timestamp":"2026-10-18T12:00:01Z","content":"Synthetic request"}` + "\n"
const user2 = `{"id":"u2","type":"user","timestamp":"2026-10-18T12:00:03Z","content":"Synthetic next request"}` + "\n"
const answer = `{"id":"g1","type":"gemini","timestamp":"2026-10-18T12:00:02Z","content":"Synthetic answer"}` + "\n"
const tool = `{"id":"g2","type":"gemini","timestamp":"2026-10-18T12:00:04Z","toolCalls":[{"id":"t1","name":"write_file","args":{"file_path":"a.go"}}]}` + "\n"

type offsets map[string]model.Offset

func (s offsets) GetOffset(p string) (model.Offset, bool, error) { o, ok := s[p]; return o, ok, nil }
func (s offsets) SetOffset(p string, o model.Offset) error       { s[p] = o; return nil }
func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func setup(t *testing.T, data string) (adapter.Env, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("GEMINI_CLI_HOME", "")
	root := filepath.Join(home, ".gemini", "tmp", "synthetic-project")
	write(t, filepath.Join(root, ".project_root"), "/workspace/synthetic\n")
	path := filepath.Join(root, "chats", "session-synthetic.jsonl")
	write(t, path, data)
	return adapter.Env{Home: home, Now: time.Date(2026, 10, 18, 12, 0, 5, 0, time.UTC), Offsets: offsets{}}, path
}
func collect(t *testing.T, a adapter.Adapter, env adapter.Env) (model.SessionObs, []adapter.Warning) {
	t.Helper()
	ss, w, err := a.Collect(context.Background(), env)
	if err != nil || len(ss) != 1 {
		t.Fatalf("observations=%+v warnings=%+v err=%v", ss, w, err)
	}
	return ss[0], w
}
func TestGeminiJSONLFold(t *testing.T) {
	for _, tc := range []struct {
		name, records, prompt string
		open                  bool
	}{
		{"user", user1, "12:00:01", true},
		{"answer", user1 + answer, "12:00:01", false},
		{"open-tool", user1 + answer + user2 + tool, "12:00:03", true},
		{"result", user1 + strings.Replace(tool, `"args":`, `"result":[],"args":`, 1), "12:00:01", false},
		{"set-messages", user1 + `{"$set":{"messages":[]}}` + "\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, _ := setup(t, header+tc.records+`{"$set":{"summary":"Synthetic title"}}`+"\n")
			a := New()
			s, _ := collect(t, a, env)
			if a.Name() != "gemini" || !reflect.DeepEqual(a.TestedVersions(), []string{"0.46"}) {
				t.Fatal("identity")
			}
			if s.ExternalID != "synthetic-session" || s.Title != "Synthetic title" || s.TurnOpen != tc.open || s.Cwd != "/workspace/synthetic" {
				t.Fatalf("fold=%+v", s)
			}
			if tc.prompt == "" {
				if !s.LastPromptAt.IsZero() {
					t.Fatal(s.LastPromptAt)
				}
			} else if s.LastPromptAt.Format("15:04:05") != tc.prompt {
				t.Fatal(s.LastPromptAt)
			}
			if s.LastPrompt != "" {
				t.Fatal("snippet without opt-in")
			}
		})
	}
}
func TestGeminiRewind(t *testing.T) {
	env, path := setup(t, header+user1+answer+user2)
	a := New()
	collect(t, a, env)
	write(t, path, header+user1+answer+user2+`{"$rewindTo":"u2"}`+"\n")
	s, _ := collect(t, a, env)
	if s.LastPromptAt.Format("15:04:05") != "12:00:01" || s.TurnOpen {
		t.Fatalf("rewind=%+v", s)
	}
	env.Offsets = offsets{}
	fresh, _ := collect(t, New(), env)
	if !reflect.DeepEqual(s, fresh) {
		t.Fatalf("incremental != fresh: %+v %+v", s, fresh)
	}
}
func TestGeminiPatchToolResult(t *testing.T) {
	for _, patch := range []string{`{"id":"g2","toolCalls":[{"id":"t1","result":[]}]}`, `{"updates":[{"id":"g2","toolCalls":[{"id":"t1","result":[]}]}]}`} {
		env, path := setup(t, header+user1+tool)
		a := New()
		s, _ := collect(t, a, env)
		if !s.TurnOpen {
			t.Fatal(s)
		}
		write(t, path, header+user1+tool+`{"$patch":`+patch+"}\n")
		s, _ = collect(t, a, env)
		if s.TurnOpen {
			t.Fatalf("patched=%+v", s)
		}
	}
}
func TestGeminiRecoveryRewrite(t *testing.T) {
	for _, rename := range []bool{true, false} {
		t.Run(fmt.Sprint(rename), func(t *testing.T) {
			env, path := setup(t, header+user1+answer+user2+tool)
			a := New()
			collect(t, a, env)
			if rename {
				write(t, path+".tmp", header+user1)
				if err := os.Rename(path+".tmp", path); err != nil {
					t.Fatal(err)
				}
			} else {
				write(t, path, header+user1)
			}
			s, _ := collect(t, a, env)
			o, _, _ := env.Offsets.GetOffset(path)
			if o.Pos != int64(len(header+user1)) {
				t.Fatalf("offset=%+v", o)
			}
			env.Offsets = offsets{}
			fresh, _ := collect(t, New(), env)
			if !reflect.DeepEqual(s, fresh) {
				t.Fatalf("recovery != fresh: %+v %+v", s, fresh)
			}
		})
	}
}
func TestGeminiSubagentParent(t *testing.T) {
	env, path := setup(t, header+user1)
	child := filepath.Join(filepath.Dir(path), "parent-session", "child.jsonl")
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	write(t, child, header+user1)
	s, _ := collect(t, New(), env)
	if s.ParentExternalID != "parent-session" {
		t.Fatal(s)
	}
}
func TestGeminiFilesTouched(t *testing.T) {
	for _, name := range []string{"write_file", "replace", "edit"} {
		for _, arg := range []string{"file_path", "absolute_path"} {
			for _, path := range []string{"a.go", "/absolute/a.go"} {
				t.Run(name+arg+path, func(t *testing.T) {
					data := strings.ReplaceAll(strings.ReplaceAll(tool, "write_file", name), "file_path", arg)
					data = strings.ReplaceAll(data, "a.go", path)
					env, _ := setup(t, header+user1+data)
					s, _ := collect(t, New(), env)
					want := path
					if !recordpath.IsAbs(want) {
						want = recordpath.Join(s.Cwd, want)
					}
					if len(s.FilesTouched) != 1 || s.FilesTouched[0].Path != want || s.FilesTouched[0].TS.IsZero() {
						t.Fatal(s.FilesTouched)
					}
				})
			}
		}
	}
}
func TestGeminiNeverWaiting(t *testing.T) {
	env, _ := setup(t, header+user1+tool)
	s, _ := collect(t, New(), env)
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{{time.Second, "working"}, {2 * time.Minute, "unknown"}, {11 * time.Minute, "stalled"}} {
		got, _, _ := derive.Label(derive.StateInput{Obs: s, Proc: &model.Proc{PID: 123}}, s.LastActivityAt.Add(tc.age), derive.Thresholds{StalledAfter: 10 * time.Minute})
		if got != tc.want {
			t.Fatalf("age=%v label=%s want=%s", tc.age, got, tc.want)
		}
	}
}
func TestGeminiHome(t *testing.T) {
	env, path := setup(t, header+user1)
	override := t.TempDir()
	t.Setenv("GEMINI_CLI_HOME", override)
	rel, err := filepath.Rel(env.Home, path)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(override, rel), header+user2)
	s, _ := collect(t, New(), env)
	if s.LastPromptAt.Format("15:04:05") != "12:00:03" {
		t.Fatal(s)
	}
}

func TestGeminiDiscoveryBaseline(t *testing.T) {
	env, path := setup(t, header+user1)
	old := env.Now.Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	env.Baseline = true
	ss, _, err := New().Collect(context.Background(), env)
	o, ok, _ := env.Offsets.GetOffset(path)
	if err != nil || len(ss) != 0 || !ok || o.Pos != int64(len(header+user1)) {
		t.Fatalf("ss=%+v offset=%+v exists=%v err=%v", ss, o, ok, err)
	}
}

func TestGeminiOutstandingTools(t *testing.T) {
	env, _ := setup(t, header+user1+tool+strings.ReplaceAll(answer, "g1", "g3"))
	s, _ := collect(t, New(), env)
	if !s.TurnOpen {
		t.Fatal("an unrelated later answer hid an outstanding tool")
	}
}

func TestGeminiSyntheticFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "gemini", "0.46", "session-synthetic.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	env, _ := setup(t, string(data))
	s, _ := collect(t, New(), env)
	if s.TurnOpen || s.LastPromptAt.Format("15:04:05") != "12:00:01" {
		t.Fatal(s)
	}
}

// prodOffsets mimics store.Offsets: stored offsets come back stamped with the
// machine and with a UTC mtime, so they are never == the tail's own value.
type prodOffsets map[string]model.Offset

func (s prodOffsets) GetOffset(p string) (model.Offset, bool, error) {
	o, ok := s[p]
	return o, ok, nil
}
func (s prodOffsets) SetOffset(p string, o model.Offset) error {
	o.MachineID, o.Mtime = "machine", o.Mtime.UTC()
	s[p] = o
	return nil
}

func TestGeminiCacheHitWithStoredOffsets(t *testing.T) {
	pad := `{"type":"padding","x":"` + strings.Repeat("x", 9<<20) + `"}` + "\n"
	env, _ := setup(t, header+pad+user1)
	env.Offsets = prodOffsets{}
	a := New()
	if _, w := collect(t, a, env); len(w) == 0 {
		t.Fatal("first read of a >8 MiB file must report skipped backlog")
	}
	if _, w := collect(t, a, env); len(w) != 0 {
		t.Fatalf("unchanged file re-read from byte 0 on the next pass: %+v", w)
	}
}

// A chat file removed between the directory scan and the read is skipped,
// not a failed pass. A dangling link reproduces the vanished file exactly.
func TestGeminiVanishedFileSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink fixture; Windows watch tests require no Developer Mode or admin rights")
	}

	env, path := setup(t, header+user1)
	if err := os.Symlink(filepath.Join(filepath.Dir(path), "deleted.jsonl"), filepath.Join(filepath.Dir(path), "session-a-vanished.jsonl")); err != nil {
		t.Fatal(err)
	}
	collect(t, New(), env)
}
