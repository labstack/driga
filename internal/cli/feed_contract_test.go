package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/labstack/driga/internal/store"
	"strings"
	"testing"
	"time"
)

func TestFeedTailUsesExclusiveSequenceFlag(t *testing.T) {
	readEnv(t)
	st := readDB(t, nil)
	for _, value := range []string{"one", "two"} {
		if err := st.Tx(context.Background(), func(tx *store.Tx) error {
			_, err := tx.AppendFeed("event", value, []byte(`{}`), time.Now())
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	var out, diagnostic bytes.Buffer
	if code := run([]string{"feed", "tail", "--after-seq", "1"}, "test", nil, &out, &diagnostic); code != 0 {
		t.Fatal(code, &diagnostic)
	}
	var row struct {
		Seq int64 `json:"seq"`
	}
	if err := json.Unmarshal(out.Bytes(), &row); err != nil || row.Seq != 2 {
		t.Fatalf("%s %v", &out, err)
	}
	out.Reset()
	diagnostic.Reset()
	if code := run([]string{"feed", "tail", "--since", "1", "--json"}, "test", nil, &out, &diagnostic); code != 2 || !json.Valid(out.Bytes()) || !strings.Contains(out.String(), "--after-seq") {
		t.Fatal(code, &out, &diagnostic)
	}
}
