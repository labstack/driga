package cli

import (
	"bytes"
	"encoding/json"
	"github.com/labstack/driga/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONSuccessAndFailureContracts(t *testing.T) {
	hookTestEnv(t)
	for _, tc := range []struct {
		args     []string
		code     int
		contains string
	}{
		{[]string{"version", "--json"}, 0, `"version":"test"`},
		{[]string{"config", "path", "--json"}, 0, `"path":`},
		{[]string{"hooks", "status", "--json"}, 2, `"code":"unsupported_output"`},
		{[]string{"--json", "bogus"}, 2, `"code":"usage_error"`},
		{[]string{"config", "show", "--json"}, 2, `"schema":2`},
	} {
		var out, diagnostic bytes.Buffer
		code := run(tc.args, "test", strings.NewReader(""), &out, &diagnostic)
		if code != tc.code || !json.Valid(out.Bytes()) || !strings.Contains(out.String(), tc.contains) {
			t.Errorf("%v: code %d stdout %s stderr %s", tc.args, code, &out, &diagnostic)
		}
	}
}

func TestDoctorJSONFailureIsOneErrorRecord(t *testing.T) {
	readEnv(t)
	if err := os.MkdirAll(config.DataDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.DataDir(), "driga.db"), []byte("corrupt database"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	code := run([]string{"doctor", "--json"}, "test", nil, &out, &diagnostic)
	if code != 1 || !json.Valid(out.Bytes()) || !strings.Contains(out.String(), `"error":`) {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &diagnostic)
	}
}
