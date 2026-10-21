package chat

import "testing"

func TestPlainCitations(t *testing.T) {
	for in, want := range map[string]string{
		"see [[session:207]] and [[session:2]].": "see #207 and #2.",
		"no markers":                             "no markers",
		"bad [[session:0]] [[session:x]]":        "bad [[session:0]] [[session:x]]",
	} {
		if got := PlainCitations(in); got != want {
			t.Errorf("PlainCitations(%q) = %q, want %q", in, got, want)
		}
	}
}

// A stream can stop mid-marker; hold back an unfinished "[[" so the terminal
// never prints a half marker it would have to take back.
func TestStreamSafePrefix(t *testing.T) {
	for in, want := range map[string]string{
		"waiting: [[sess":            "waiting: ",
		"waiting: [[session:207]] x": "waiting: [[session:207]] x",
		"done [[session:2]] and [":   "done [[session:2]] and ",
		"plain text":                 "plain text",
	} {
		if got := StreamSafePrefix(in); got != want {
			t.Errorf("StreamSafePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
