package tui

import (
	"testing"
)

func TestHelpAndUserMessagesWrapWords(t *testing.T) {
	if got := wrapText("ordinary words remain together", 15); got != "ordinary words\nremain together" {
		t.Fatal(got)
	}
	if got := wrapText("12345678901234567890", 10); got != "1234567890\n1234567890" {
		t.Fatal("long tokens must still fit", got)
	}
}
