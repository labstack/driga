package chat

import (
	"context"
	"testing"
	"time"
)

func TestPromptHistoryContainsOnlyIntervals(t *testing.T) {
	b, _, st := fixture(t, false)
	done := make(chan error, 1)
	go func() { _, err := b.WaitPrompt(context.Background(), testPrompt("history")); done <- err }()
	waitPrompt(t, b, "history")
	deadline := time.Now().Add(3 * time.Second)
	for {
		periods, err := st.PromptIntervals(time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if len(periods) == 1 && periods[0].EndedAt == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pending prompt not in history")
		}
		time.Sleep(time.Millisecond)
	}
	if err := b.answerPrompt("history", true); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	periods, err := st.PromptIntervals(time.Now().Add(-time.Hour))
	if err != nil || len(periods) != 1 || periods[0].EndedAt == nil {
		t.Fatalf("retired intervals %+v %v", periods, err)
	}
}
