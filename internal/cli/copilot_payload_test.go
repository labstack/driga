package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestCopilotResultFilter(t *testing.T) {
	for _, result := range []string{`"text with \\\" escapes"`, `{"nested":[true,false,null,1.2e-3,{"text":"result"}]}`, `123` + strings.Repeat("0", 1024)} {
		input := `{"tool\u0052esult":` + result + `,"sessionId":"s","timestamp":1,"cwd":"/repo"}`
		got, err := hookReadCopilotInput(strings.NewReader(input))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(got, []byte("nested")) || len(got) > 128 || !json.Valid(got) {
			t.Fatalf("result retained: %.200s", got)
		}
	}
	for _, input := range []string{
		`{"toolResult":"unterminated}`, `{"toolResult":[true,]}`, `{"toolResult":{"a":1,}}`,
		`{"toolResult":"bad\q"}`, `{"toolResult":01}`, `{"toolResult":1e}`, `{"toolResult":true} false`,
	} {
		if got, err := hookReadCopilotInput(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted invalid JSON: %s => %s", input, got)
		}
	}
}

type copilotFillReader struct{ read int64 }

func (r *copilotFillReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 'x'
	}
	r.read += int64(len(b))
	return len(b), nil
}
func TestCopilotPayloadBounds(t *testing.T) {
	if _, err := hookReadCopilotInput(strings.NewReader(`{"prompt":"` + strings.Repeat("x", hookPayloadLimit) + `"}`)); err == nil {
		t.Fatal("unbounded metadata")
	}
	fill := &copilotFillReader{}
	input := io.MultiReader(strings.NewReader(`{"toolResult":"`), fill)
	p := copilotJSONFilter{r: bufio.NewReader(input)}
	if err := p.value(true, 0); err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatal(err)
	}
	if fill.read > copilotInputLimit+4096 || p.out.Len() > 100 {
		t.Fatal("unbounded discarded input", fill.read, p.out.Len())
	}
}
