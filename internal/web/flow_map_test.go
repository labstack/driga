package web

import (
	"encoding/json"
	"fmt"
	"github.com/labstack/driga/internal/view"
	"testing"
)

func TestFlowJSONAuthAndShape(t *testing.T) {
	s, _, id, _ := fixture(t)
	for _, tc := range []struct {
		method, path, token string
		status              int
	}{{"GET", "/flow.json", "", 401}, {"POST", "/flow.json", s.token, 405}, {"GET", "/flow.json?id=bad", s.token, 400}, {"GET", "/flow.json?minutes=3", s.token, 400}, {"GET", fmt.Sprintf("/flow.json?id=%d", id), s.token, 200}, {"HEAD", "/flow.json", s.token, 200}} {
		w := request(s, tc.method, tc.path, tc.token)
		if w.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body)
		}
		if tc.status == 200 && tc.method == "GET" {
			var e struct {
				Schema int
				Data   view.FlowView
			}
			if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
				t.Fatal(err)
			}
			if e.Schema != 2 || e.Data.Lead == nil || e.Data.Lead.Events == nil || e.Data.Attention.Waits == nil {
				t.Fatalf("shape: %s", w.Body)
			}
			if w.Header().Get("Content-Security-Policy") != csp {
				t.Fatal(w.Header())
			}
		}
	}
}
