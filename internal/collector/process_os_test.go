package collector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

func TestRecordedProcessOS(t *testing.T) {
	for _, sourceOS := range []string{"linux", "windows"} {
		for _, shape := range []string{"linux", "windows"} {
			t.Run(sourceOS+"/"+shape, func(t *testing.T) {
				d, st, _, a := setup(t)
				a.obs = nil
				start := d.Now().Add(-time.Hour)
				root := model.Proc{PID: 200, PPID: 700, StartTime: start, Cmdline: "claude mcp serve", Argv: []string{"claude", "mcp", "serve"}}
				procs := []model.Proc{root}
				if shape == "linux" {
					procs[0].PPID = 1
				}
				dir := t.TempDir()
				for name, data := range map[string]any{
					"procs.json":   procs,
					"machine.json": map[string]any{"os": sourceOS, "cores": 2},
				} {
					b, err := json.Marshal(data)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
						t.Fatal(err)
					}
				}
				fake, err := platform.LoadFake(dir)
				if err != nil {
					t.Fatal(err)
				}
				d.Plat = fake
				pass(t, d, st, Plan{Procs: true})
				m, err := st.Machine()
				if err != nil || m.OS != sourceOS {
					t.Fatalf("source OS: %+v %v", m, err)
				}
				orphans, err := st.Orphans()
				want := 0
				if shape == "linux" || sourceOS == "windows" {
					want = 1
				}
				if err != nil || len(orphans) != want {
					t.Fatalf("orphans: %+v %v, want %d", orphans, err, want)
				}
				if want == 1 {
					evidence := "ppid=1"
					if sourceOS == "windows" {
						evidence = "parent process gone"
						if !orphans[0].ProcessStarts[200].Equal(start) {
							t.Fatal("Windows member identity not pinned")
						}
					}
					if !strings.Contains(strings.Join(orphans[0].Evidence, ";"), evidence) {
						t.Fatal(orphans[0].Evidence)
					}
				}
			})
		}
	}
}

func TestMachineOSRecordedWithoutDueSamples(t *testing.T) {
	d, st, _, _ := setup(t)
	pass(t, d, st, Plan{})
	m, err := st.Machine()
	if err != nil || m.OS != runtime.GOOS {
		t.Fatalf("machine OS without samples: %+v %v", m, err)
	}
}
