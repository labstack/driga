package web

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/store"
)

func TestStartedFromAgentPages(t *testing.T) {
	s, st, id, _ := fixture(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		a, err := tx.Session(id)
		if err != nil {
			return err
		}
		a.StartedFrom = "Fixture <Editor>"
		_, err = tx.UpsertSession(a)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/agents", fmt.Sprintf("/agents/%d", id)} {
		w := request(s, "GET", path)
		if w.Code != 200 || !strings.Contains(w.Body.String(), " · from Fixture &lt;Editor&gt;") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
}
