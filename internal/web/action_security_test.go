package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"strings"
	"testing"
	"time"
)

func authorizeAction(t *testing.T, s *Server, body string) string {
	t.Helper()
	var in actionRequest
	if err := json.Unmarshal([]byte(body), &in); err != nil {
		t.Fatal(err)
	}
	in.Token = ""
	raw, _ := json.Marshal(in)
	w := postJSON(s, "/app/confirm", `{"message":"Proceed?","request":`+string(raw)+`}`)
	var out struct{ Token string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Token == "" {
		t.Fatalf("confirm: %d %s", w.Code, w.Body)
	}
	in.Token = out.Token
	raw, _ = json.Marshal(in)
	return string(raw)
}

func TestDestructiveActionsRequireBoundOneTimeToken(t *testing.T) {
	s, st, id, _ := fixture(t)
	calls := 0
	s.opts.Action = func(context.Context, string, json.RawMessage) error { calls++; return nil }
	s.opts.Confirm = func(context.Context, string) bool { return true }
	raw, _ := json.Marshal(actionRequest{Action: "kill", SessionID: id, Machine: st.MachineID()})
	body := string(raw)
	for _, forged := range []string{strings.TrimSuffix(body, "}") + `,"confirmed":true}`, strings.TrimSuffix(body, "}") + `,"token":"forged"}`, body} {
		if w := postJSON(s, "/app/action", forged); w.Code == 200 {
			t.Fatalf("forged allowed: %s", forged)
		}
	}
	valid := authorizeAction(t, s, body)
	changed := strings.Replace(valid, `"action":"kill"`, `"action":"cleanup","id":"other"`, 1)
	if w := postJSON(s, "/app/action", changed); w.Code == 200 {
		t.Fatal("changed action allowed")
	}
	valid = authorizeAction(t, s, body)
	if w := postJSON(s, "/app/action", valid); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := postJSON(s, "/app/action", valid); w.Code == 200 {
		t.Fatal("replayed token allowed")
	}
	expired := authorizeAction(t, s, body)
	s.mu.Lock()
	for k, c := range s.confirmations {
		c.until = time.Now().Add(-time.Second)
		s.confirmations[k] = c
	}
	s.mu.Unlock()
	if w := postJSON(s, "/app/action", expired); w.Code == 200 {
		t.Fatal("expired token allowed")
	}
	s.opts.Confirm = func(context.Context, string) bool { return false }
	if w := postJSON(s, "/app/confirm", `{"message":"Proceed?","request":`+body+`}`); strings.Contains(w.Body.String(), `"token"`) {
		t.Fatal("denied issued token")
	}
	if calls != 1 {
		t.Fatalf("actions called %d", calls)
	}
}

func TestAppRejectsForgedHostAndOrigin(t *testing.T) {
	s, _ := New(appSource{}, Options{})
	for _, tc := range []struct {
		method, target, host, origin string
		allowed                      bool
	}{
		{"GET", "/app/", "evil.example:47801", "", false},
		{"GET", "/app/", "localhost.evil:47801", "", false},
		{"POST", "/chat/api/new", "127.0.0.1:47801", "http://evil.example", false},
		{"POST", "/chat/api/new", "127.0.0.1:47801", "http://127.0.0.1:5173", false},
		{"POST", "/chat/api/new", "127.0.0.1:47801", "", false},
		{"POST", "/chat/api/new", "127.0.0.1:47801", "null", false},
		{"GET", "/app/", "127.0.0.1:47801", "", true},
		{"POST", "/app/confirm", "[::1]:47801", "http://[::1]:47801", true},
		{"POST", "mygo://localhost/app/confirm", "localhost", "mygo://localhost", true},
		{"POST", "/app/confirm", "mygo.localhost", "http://mygo.localhost", false},
	} {
		r := httptest.NewRequest(tc.method, tc.target, strings.NewReader(`{}`))
		r.Host = tc.host
		if strings.HasPrefix(tc.target, "mygo://") && tc.allowed {
			r.RemoteAddr = "mygo:1"
		}
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if (w.Code != 403) != tc.allowed {
			t.Errorf("%+v: %d", tc, w.Code)
		}
	}
}

func TestConfirmationBindsEveryActionArgument(t *testing.T) {
	s, st, session, _ := fixture(t)
	s.opts.Confirm = func(context.Context, string) bool { return true }
	calls := 0
	s.opts.Action = func(context.Context, string, json.RawMessage) error { calls++; return nil }
	var other int64
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		var err error
		other, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "second", Cwd: "/fixture", LastActivityAt: time.Now()}, Label: "idle"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	local := st.MachineID()
	kill := actionRequest{Action: "kill", SessionID: session, Machine: local}
	for _, tc := range []struct {
		name     string
		approved actionRequest
		change   func(*actionRequest)
	}{
		{name: "machine", approved: kill, change: func(r *actionRequest) { r.Machine = "other-machine" }},
		{name: "session", approved: kill, change: func(r *actionRequest) { r.SessionID = other }},
		{name: "force", approved: kill, change: func(r *actionRequest) { r.Force = true }},
		{name: "action", approved: kill, change: func(r *actionRequest) { r.Action = "cleanup"; r.ID = "wt:other" }},
		{name: "cleanup target", approved: actionRequest{Action: "cleanup", ID: "wt:fixture", Machine: local}, change: func(r *actionRequest) { r.ID = "wt:other" }},
		{name: "acknowledge all", approved: actionRequest{Action: "ack_inbox", ID: "waiting"}, change: func(r *actionRequest) { r.ID = ""; r.All = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.approved)
			authorized := authorizeAction(t, s, string(raw))
			var original actionRequest
			if err := json.Unmarshal([]byte(authorized), &original); err != nil {
				t.Fatal(err)
			}
			forged := original
			tc.change(&forged)
			// Check the binding itself, even where snapshot validation also rejects
			// the forged target. A mismatched attempt consumes that one-use token.
			if s.consumeConfirmation(forged) {
				t.Fatal("confirmation accepted changed argument")
			}
			if s.consumeConfirmation(original) {
				t.Fatal("mismatched attempt left reusable token")
			}
			authorized = authorizeAction(t, s, string(raw))
			if err := json.Unmarshal([]byte(authorized), &forged); err != nil {
				t.Fatal(err)
			}
			tc.change(&forged)
			body, _ := json.Marshal(forged)
			if w := postJSON(s, "/app/action", string(body)); w.Code != http.StatusBadRequest {
				t.Fatalf("forged action: %d %s", w.Code, w.Body)
			}
			if calls != 0 {
				t.Fatalf("forged request reached action callback %d times", calls)
			}
		})
	}
}

func TestCleanupAndAcknowledgeAllRequireNativeToken(t *testing.T) {
	s, st, _, _ := fixture(t)
	s.opts.Confirm = func(context.Context, string) bool { return true }
	calls := 0
	s.opts.Action = func(context.Context, string, json.RawMessage) error { calls++; return nil }
	for _, in := range []actionRequest{{Action: "cleanup", ID: "wt:fixture", Machine: st.MachineID()}, {Action: "ack_inbox", All: true}} {
		raw, _ := json.Marshal(in)
		if w := postJSON(s, "/app/action", string(raw)); w.Code != http.StatusBadRequest {
			t.Fatalf("unconfirmed %s: %d %s", in.Action, w.Code, w.Body)
		}
		approved := authorizeAction(t, s, string(raw))
		if w := postJSON(s, "/app/action", approved); w.Code != http.StatusOK {
			t.Fatalf("confirmed %s: %d %s", in.Action, w.Code, w.Body)
		}
		if w := postJSON(s, "/app/action", approved); w.Code != http.StatusBadRequest {
			t.Fatalf("replayed %s: %d %s", in.Action, w.Code, w.Body)
		}
	}
	if calls != 2 {
		t.Fatalf("action calls %d, want each approved request once", calls)
	}
}

func TestEveryEndpointRejectsForgedOriginsBeforeRouting(t *testing.T) {
	endpoints := []struct{ method, path string }{
		{"GET", "/app/"}, {"GET", "/chat/api/history"}, {"GET", "/chat/api/prompts"}, {"GET", "/static/fonts/mono.woff2"},
		{"POST", "/chat/api/decide"}, {"POST", "/chat/api/prompt-answer"}, {"POST", "/app/confirm"}, {"POST", "/app/action"}, {"POST", "/app/open-external"}, {"POST", "/app/api/list"}, {"POST", "/app/api/edit"}, {"POST", "/app/api/delete"},
	}
	attacks := []struct{ name, host, origin string }{
		{"DNS rebinding", "attacker.example:47801", "http://attacker.example:47801"},
		{"deceptive suffix", "localhost.attacker.example:47801", "http://localhost.attacker.example:47801"},
		{"forged origin", "127.0.0.1:47801", "https://attacker.example"},
		{"other loopback port", "127.0.0.1:47801", "http://127.0.0.1:5173"},
		{"other loopback host", "127.0.0.1:47801", "http://localhost:47801"},
		{"opaque origin", "127.0.0.1:47801", "null"},
	}
	for _, endpoint := range endpoints {
		for _, attack := range attacks {
			t.Run(endpoint.method+endpoint.path+"/"+attack.name, func(t *testing.T) {
				s, err := New(appSource{}, Options{})
				if err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(`{}`))
				r.Host = attack.host
				r.Header.Set("Origin", attack.origin)
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				if w.Code != http.StatusForbidden {
					t.Fatalf("forged request routed: %d", w.Code)
				}
				if !s.polled.IsZero() {
					t.Fatal("forged request counted as app activity")
				}
			})
		}
	}
	for _, path := range []string{"/chat/api/decide", "/chat/api/prompt-answer", "/app/action", "/app/api/edit"} {
		t.Run("missing origin"+path, func(t *testing.T) {
			s, err := New(appSource{}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
			r.Host = "127.0.0.1:47801"
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("missing Origin routed: %d", w.Code)
			}
		})
	}
}

func TestNativeProtocolOriginsRequireToolkitTransport(t *testing.T) {
	for _, tc := range []struct {
		name, target, host, remote, origin string
		allowed                            bool
	}{
		{"native macOS and Linux", "mygo://localhost/app/confirm", "localhost", "mygo:1", "mygo://localhost", true},
		{"transformed native Windows", "mygo://localhost/app/confirm", "localhost", "mygo:2", "http://mygo.localhost", true},
		{"network forged private scheme", "mygo://localhost/app/confirm", "localhost", "127.0.0.1:12345", "mygo://localhost", false},
		{"network forged Windows request", "mygo://localhost/app/confirm", "localhost", "127.0.0.1:12345", "http://mygo.localhost", false},
		{"raw HTTP Windows address", "http://mygo.localhost/app/confirm", "mygo.localhost", "127.0.0.1:12345", "http://mygo.localhost", false},
		{"HTTP loopback private origin", "http://127.0.0.1:47801/app/confirm", "127.0.0.1:47801", "127.0.0.1:12345", "mygo://localhost", false},
		{"HTTP loopback Windows origin", "http://127.0.0.1:47801/app/confirm", "127.0.0.1:47801", "127.0.0.1:12345", "http://mygo.localhost", false},
		{"native missing Origin", "mygo://localhost/app/confirm", "localhost", "mygo:1", "", false},
		{"native opaque Origin", "mygo://localhost/app/confirm", "localhost", "mygo:1", "null", false},
		{"native unrelated Origin", "mygo://localhost/app/confirm", "localhost", "mygo:1", "https://evil.example", false},
		{"native wrong host", "mygo://evil.example/app/confirm", "evil.example", "mygo:1", "mygo://localhost", false},
		{"native host port", "mygo://localhost:80/app/confirm", "localhost:80", "mygo:1", "mygo://localhost", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dialogs := 0
			s, err := New(appSource{}, Options{Confirm: func(context.Context, string) bool { dialogs++; return true }})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", tc.target, strings.NewReader(`{"message":"Proceed?"}`))
			r.Host = tc.host
			r.RemoteAddr = tc.remote
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", "application/json")
			// Headers cannot forge the transport identity the toolkit stamps.
			r.Header.Set("Remote-Addr", "mygo:1")
			r.Header.Set("X-Forwarded-For", "mygo:1")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if tc.allowed {
				if w.Code != http.StatusOK || dialogs != 1 {
					t.Fatalf("native request rejected: %d dialogs=%d", w.Code, dialogs)
				}
			} else if w.Code != http.StatusForbidden || dialogs != 0 {
				t.Fatalf("forged native request accepted: %d dialogs=%d", w.Code, dialogs)
			}
		})
	}
}
