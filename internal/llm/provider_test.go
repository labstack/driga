package llm

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunProviderKeepsAuthHomeButRemovesAmbientAuthority(t *testing.T) {
	dir := fake(t, `printf '%s\n' "$DRIGA_INTERNAL" "$COPILOT_HOME" "$COPILOT_AUTO_UPDATE" "$CODEX_EXEC_SERVER_URL"
if [ -n "$DRIGA_CHAT_TOKEN$DRIGA_CHAT_SOCKET$COPILOT_ALLOW_ALL$COPILOT_PROVIDER_API_KEY_COMMAND$BASH_ENV$NODE_OPTIONS$driga_chat_token$Driga_Chat_Socket$copilot_allow_all$Node_Options$CODEX_EXEC_SERVER_NOISE_REGISTRY_URL$codex_exec_server_noise_auth_token$CodeX_Exec_Server_Noise_Environment_Id" ]; then exit 23; fi
/bin/cat`)
	authHome := t.TempDir()
	t.Setenv("COPILOT_HOME", authHome)
	for _, key := range []string{"DRIGA_CHAT_TOKEN", "DRIGA_CHAT_SOCKET", "COPILOT_ALLOW_ALL", "COPILOT_PROVIDER_API_KEY_COMMAND", "BASH_ENV", "NODE_OPTIONS", "driga_chat_token", "Driga_Chat_Socket", "copilot_allow_all", "Node_Options", "CODEX_EXEC_SERVER_NOISE_REGISTRY_URL", "codex_exec_server_noise_auth_token", "CodeX_Exec_Server_Noise_Environment_Id"} {
		t.Setenv(key, "fixture-do-not-execute")
	}
	var out bytes.Buffer
	err := RunProvider(context.Background(), filepath.Join(dir, "claude"), t.TempDir(), nil, []string{"CODEX_EXEC_SERVER_URL=none"}, strings.NewReader("input"), &out, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	if want := "1\n" + authHome + "\nfalse\nnone\ninput"; out.String() != want {
		t.Fatalf("output=%q want=%q", out.String(), want)
	}
}
