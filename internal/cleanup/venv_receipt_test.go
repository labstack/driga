//go:build !windows

package cleanup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/logfile"
)

func venvReceiptFixture(t *testing.T) (Env, Candidate, string) {
	t.Helper()
	e, _, repo, _ := buildFixture(t)
	put(t, filepath.Join(repo, "pyproject.toml"), "")
	put(t, filepath.Join(repo, ".gitignore"), ".venv/\nnode_modules/\n")
	path := filepath.Join(repo, ".venv")
	put(t, filepath.Join(path, "lib/python3.13/site-packages/requests-2.32.0.dist-info/METADATA"), "")
	return e, listed(t, e, path), path
}

func TestVenvReceiptFailurePreservesEnvironment(t *testing.T) {
	for _, failure := range []string{"open", "rotation"} {
		t.Run(failure, func(t *testing.T) {
			e, c, path := venvReceiptFixture(t)
			log := filepath.Join(e.DataDir, "cleanup-reconstruction.log")
			blocked := log
			if failure == "rotation" {
				put(t, log, strings.Repeat("x", int(logfile.MaxSize)))
				blocked += ".1"
			}
			if err := os.MkdirAll(blocked, 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil {
				t.Fatal("cleanup accepted an unwritable reconstruction receipt")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("venv removed before receipt persisted", err)
			}
			b, err := os.ReadFile(filepath.Join(e.DataDir, "cleanup.log"))
			if err != nil || !strings.Contains(string(b), `"error":`) {
				t.Fatalf("missing final failure outcome: %s %v", b, err)
			}
		})
	}
}

func TestVenvReceiptAndFinalOutcome(t *testing.T) {
	e, c, path := venvReceiptFixture(t)
	r, err := Apply(context.Background(), e, c, ApplyOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("venv survived", err)
	}
	log := filepath.Join(e.DataDir, "cleanup-reconstruction.log")
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal("missing durable reconstruction receipt", err)
	}
	var receipt struct {
		Phase    string   `json:"phase"`
		ID       string   `json:"candidate_id"`
		Path     string   `json:"path"`
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(b, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Phase != "prepared" || receipt.ID != c.ID || receipt.Path != path || len(receipt.Packages) != 1 || receipt.Packages[0] != "requests-2.32.0" {
		t.Fatalf("wrong reconstruction record: %s", b)
	}
	info, err := os.Stat(log)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("receipt is not private", info, err)
	}
	b, err = os.ReadFile(filepath.Join(e.DataDir, "cleanup.log"))
	if err != nil {
		t.Fatal(err)
	}
	var final struct {
		Result Result `json:"result"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(b, &final); err != nil {
		t.Fatal("preparation polluted final outcome log", err)
	}
	if final.Error != "" || len(final.Result.Packages) != 1 || final.Result.ID != r.ID {
		t.Fatalf("wrong final outcome: %s", b)
	}
}

func TestVenvDryRunDoesNotPrepareReceipt(t *testing.T) {
	e, c, path := venvReceiptFixture(t)
	if _, err := Apply(context.Background(), e, c, ApplyOpts{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.DataDir, "cleanup-reconstruction.log")); !os.IsNotExist(err) {
		t.Fatal("dry run prepared removal", err)
	}
}

func TestVenvOversizedReceiptPreservesEnvironment(t *testing.T) {
	e, _, path := venvReceiptFixture(t)
	for i := 0; i < 400; i++ {
		name := fmt.Sprintf("%03d-%s.dist-info", i, strings.Repeat("p", 180))
		if err := os.Mkdir(filepath.Join(path, "lib/python3.13/site-packages", name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	c := listed(t, e, path)
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil || !strings.Contains(err.Error(), "record exceeds") {
		t.Fatal("oversized reconstruction accepted", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("venv removed without complete receipt", err)
	}
	if _, err := os.Stat(filepath.Join(e.DataDir, "cleanup-reconstruction.log")); !os.IsNotExist(err) {
		t.Fatal("oversized record written", err)
	}
}

func TestVenvReceiptSurvivesEarlierPartialAppend(t *testing.T) {
	e, c, path := venvReceiptFixture(t)
	log := filepath.Join(e.DataDir, "cleanup-reconstruction.log")
	put(t, log, `{"phase":"prepared","packages":["incomplete`)
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("venv survived", err)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	valid := 0
	for _, line := range strings.Split(string(b), "\n") {
		var record struct {
			ID       string   `json:"candidate_id"`
			Packages []string `json:"packages"`
		}
		if json.Unmarshal([]byte(line), &record) == nil && record.ID == c.ID && len(record.Packages) == 1 && record.Packages[0] == "requests-2.32.0" {
			valid++
		}
	}
	if valid != 1 {
		t.Fatalf("acknowledged reconstruction record lost after partial append: %q", b)
	}
}
