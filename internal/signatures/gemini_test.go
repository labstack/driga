package signatures_test

import (
	"github.com/labstack/driga/internal/signatures"
	"testing"
)

func TestGeminiSignature(t *testing.T) {
	s, err := signatures.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"gemini", "/opt/bin/gemini --resume", "node /opt/node_modules/@google/gemini-cli/dist/index.js", "/usr/bin/node /opt/@google/gemini-cli/bundle/index.js"} {
		got, ok := s.Match(cmd, "", nil)
		if !ok || got.Agent != "gemini" || got.Role != signatures.RoleInteractive {
			t.Fatalf("%s: %+v %v", cmd, got, ok)
		}
	}
	for _, cmd := range []string{"echo gemini", "vim /opt/@google/gemini-cli/dist/index.js", "gemini-other"} {
		if got, ok := s.Match(cmd, "", nil); ok {
			t.Fatalf("false match: %s %+v", cmd, got)
		}
	}
}
