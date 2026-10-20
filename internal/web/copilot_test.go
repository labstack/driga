package web

import "testing"

func TestCopilotInitial(t *testing.T) {
	initial := templateFuncs()["initial"].(func(string) string)
	if initial("copilot") == initial("claude") || initial("copilot") == "?" {
		t.Fatal("ambiguous Copilot initial")
	}
}
