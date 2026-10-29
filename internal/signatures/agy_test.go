package signatures_test

import (
	"github.com/labstack/driga/internal/signatures"
	"testing"
)

func TestAgySignature(t *testing.T) {
	s, err := signatures.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"agy", "/opt/bin/agy --resume"} {
		got, ok := s.Match(cmd, "", nil)
		if !ok || got.Agent != "agy" || got.Role != signatures.RoleInteractive {
			t.Fatalf("%s: %+v %v", cmd, got, ok)
		}
	}
	if got, ok := s.Match("", "/opt/bin/agy", nil); !ok || got.Agent != "agy" {
		t.Fatal(got, ok)
	}
	for _, cmd := range []string{"echo agy", "agy-other"} {
		if got, ok := s.Match(cmd, "", nil); ok {
			t.Fatal(got)
		}
	}
}
