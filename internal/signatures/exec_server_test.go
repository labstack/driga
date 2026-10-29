package signatures_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

func TestCodexExecServerCommandMode(t *testing.T) {
	b, err := os.ReadFile("../../testdata/fixtures/codex/helpers/exec-server.json")
	if err != nil {
		t.Fatal(err)
	}
	var procs []model.Proc
	if err := json.Unmarshal(b, &procs); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		argv []string
		want string
		role signatures.Role
	}{
		{"ChatGPT fixture", procs[1].Argv, "codex-exec-server", signatures.RoleHelper},
		{"bare", []string{"codex", "exec-server"}, "codex-exec-server", signatures.RoleHelper},
		{"config overrides", []string{"codex", "-c", `roots=["/tmp/a", "/tmp/b"]`, `--config=label="exec-server words"`, "-c=one=true", "exec-server"}, "codex-exec-server", signatures.RoleHelper},
		{"Windows", []string{`C:\Program Files\Codex\CODEX.EXE`, "exec-server"}, "codex-exec-server", signatures.RoleHelper},
		{"versioned", []string{"/opt/codex/versions/1.2.3", "exec-server"}, "codex-exec-server", signatures.RoleHelper},
		{"exec prompt", []string{"codex", "exec", "exec-server"}, "codex-exec", signatures.RoleHeadless},
		{"configured exec", []string{"codex", "-c", `label="exec-server"`, "exec", "task"}, "codex-exec", signatures.RoleHeadless},
		{"config value", []string{"codex", "-c", "exec-server"}, "codex-interactive", signatures.RoleInteractive},
		{"escaped config value", []string{"codex", "-c", `label="say \" exec-server words"`}, "codex-interactive", signatures.RoleInteractive},
		{"prompt token", []string{"codex", "exec-server task"}, "codex-interactive", signatures.RoleInteractive},
		{"similar mode", []string{"codex", "exec-server-other"}, "codex-interactive", signatures.RoleInteractive},
		{"terminator", []string{"codex", "--", "exec-server"}, "codex-interactive", signatures.RoleInteractive},
		{"path argument", []string{"codex", "--resume", "/tmp/exec-server"}, "codex-interactive", signatures.RoleInteractive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sig, ok := signatures.Builtin().Match(strings.Join(tc.argv, " "), tc.argv[0], tc.argv)
			if !ok || sig.Name != tc.want || sig.Agent != "codex" || sig.Role != tc.role {
				t.Errorf("Match = %s/%s/%s, %t; want %s/codex/%s", sig.Name, sig.Agent, sig.Role, ok, tc.want, tc.role)
			}
		})
	}
	t.Run("unavailable argv", func(t *testing.T) {
		sig, ok := signatures.Builtin().Match("codex exec-server", "codex", nil)
		if !ok || sig.Role != signatures.RoleInteractive {
			t.Errorf("inferred helper without argv: %+v, %t", sig, ok)
		}
	})
}
