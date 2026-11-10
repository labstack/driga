package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/store"
)

func TestProviderOverrideDoesNotChangeDefault(t *testing.T) {
	var probes []string
	p := &providers{preference: "copilot", probe: func(_ context.Context, name string) (Agent, error) {
		probes = append(probes, name)
		return providerAgentFunc(func(context.Context, Request, func(Delta) error) error { return nil }), nil
	}}
	if got, err := p.SelectProvider(context.Background(), "auto"); err != nil || got != "claude" {
		t.Fatalf("auto override = %q, %v", got, err)
	}
	if got, err := p.Select(context.Background()); err != nil || got != "copilot" {
		t.Fatalf("default after override = %q, %v", got, err)
	}
	before := len(probes)
	if _, err := p.SelectProvider(context.Background(), "unknown"); err == nil || len(probes) != before {
		t.Fatalf("invalid preference probed providers: %v err=%v", probes, err)
	}
}

func TestOpenRemoteUsesRequestedProviderForNewConversation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native socket fixture; HTTP provider contract is covered separately")
	}
	p := &providers{preference: "claude", probe: func(_ context.Context, name string) (Agent, error) {
		return providerAgentFunc(func(_ context.Context, r Request, emit func(Delta) error) error {
			if r.Provider != name {
				return errors.New("request provider changed")
			}
			if r.Resume != "" && r.Resume != name+"-session" {
				return errors.New("resume crossed provider")
			}
			return emit(Delta{Text: "fixture answer", SessionID: name + "-session"})
		}), nil
	}}
	b, st := providerBroker(t, p)
	ctx := context.Background()
	dir := t.TempDir()
	stop, err := Listen(ctx, dir, b)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	client, ephemeral, closeClient, err := Open(ctx, dir, false, "codex")
	if err != nil || ephemeral {
		t.Fatalf("remote open: ephemeral=%v err=%v", ephemeral, err)
	}
	defer closeClient()
	c, err := client.Create(ctx)
	if err != nil || c.Agent != "codex" {
		t.Fatalf("override conversation=%+v err=%v", c, err)
	}
	if saved, err := st.ChatConversation(c.ID); err != nil || saved.Agent != "codex" {
		t.Fatalf("persisted=%+v err=%v", saved, err)
	}
	if err := client.Send(ctx, c.ID, "first", Context{}); err != nil {
		t.Fatal(err)
	}
	settled(t, b, c.ID)
	oldClient, err := Dial(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer oldClient.Close()
	defaultConversation, err := oldClient.Create(ctx)
	if err != nil || defaultConversation.Agent != "claude" {
		t.Fatalf("old client default=%+v err=%v", defaultConversation, err)
	}
	if err := client.Send(ctx, c.ID, "resume", Context{}); err != nil {
		t.Fatal(err)
	}
	state := settled(t, b, c.ID)
	if state.Conversation.Agent != "codex" || state.Conversation.SessionID != "codex-session" {
		t.Fatalf("override lost pin: %+v", state.Conversation)
	}
	for _, message := range state.Messages {
		if message.Status == "error" {
			t.Fatalf("pinned turn failed: %+v", message)
		}
	}
	client.(*Remote).provider = "unknown"
	if _, err := client.Create(ctx); err == nil {
		t.Fatal("unknown remote preference accepted")
	}
	if rows, err := st.ChatConversations(); err != nil || len(rows) != 2 {
		t.Fatalf("failed override created row: %+v err=%v", rows, err)
	}
}

func TestConversationProviderRequestContract(t *testing.T) {
	p := &providers{preference: "claude", probe: func(_ context.Context, name string) (Agent, error) {
		if name == "copilot" {
			return nil, errors.New("fixture unavailable")
		}
		return providerAgentFunc(func(context.Context, Request, func(Delta) error) error { return nil }), nil
	}}
	b, st := providerBroker(t, p)
	b.clientToken = "fixture-token"
	for _, tt := range []struct {
		body, want string
		ok         bool
	}{
		{"", "claude", true},
		{"{}", "claude", true},
		{`{"provider":"codex"}`, "codex", true},
		{`{"provider":"auto"}`, "claude", true},
		{`{"provider":"unknown"}`, "", false},
		{`{"provider":"copilot"}`, "", false},
		{`{"provider":42}`, "", false},
		{`{"provider":"codex","extra":true}`, "", false},
		{`{"provider":"codex"} {}`, "", false},
	} {
		t.Run(tt.body, func(t *testing.T) {
			before, err := st.ChatConversations()
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/conversations", strings.NewReader(tt.body))
			req.Header.Set("Authorization", "fixture-token")
			response := httptest.NewRecorder()
			handler(b).ServeHTTP(response, req)
			if (response.Code == http.StatusOK) != tt.ok {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
			after, err := st.ChatConversations()
			if err != nil {
				t.Fatal(err)
			}
			if !tt.ok {
				if len(after) != len(before) {
					t.Fatal("rejected request persisted a conversation")
				}
				return
			}
			var c createConversationResponse
			var requested struct {
				Provider string `json:"provider"`
			}
			if tt.body != "" {
				_ = json.Unmarshal([]byte(tt.body), &requested)
			}
			if err := json.Unmarshal(response.Body.Bytes(), &c); err != nil || c.Agent != tt.want || c.ProviderPreference != requested.Provider || len(after) != len(before)+1 {
				t.Fatalf("created=%+v err=%v", c, err)
			}
		})
	}
}

func TestRemoteProviderOverrideRefusesOlderServiceMismatch(t *testing.T) {
	for _, tt := range []struct{ preference, ack string }{{"codex", ""}, {"auto", ""}, {"codex", "codex"}} {
		t.Run(tt.preference+"/ack="+tt.ack, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Provider string `json:"provider"`
				}
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Provider != tt.preference {
					t.Errorf("remote override=%+v err=%v", in, err)
				}
				// The previous service ignored the new request body and chose its default.
				_ = json.NewEncoder(w).Encode(createConversationResponse{Conversation: store.Conversation{ID: "old-service", Agent: "claude"}, ProviderPreference: tt.ack})
			}))
			defer server.Close()
			remote := &Remote{provider: tt.preference, http: server.Client()}
			// The transport uses a fixed local URL; direct all requests to this fixture.
			remote.http.Transport = fixtureProviderTransport{target: server.URL, base: http.DefaultTransport}
			if _, err := remote.Create(context.Background()); err == nil || !strings.Contains(err.Error(), "restart the service") {
				t.Fatalf("old service mismatch err=%v", err)
			}
		})
	}
}

type fixtureProviderTransport struct {
	target string
	base   http.RoundTripper
}

func (f fixtureProviderTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	url, err := url.Parse(f.target)
	if err != nil {
		return nil, err
	}
	copy.URL.Scheme, copy.URL.Host = url.Scheme, url.Host
	return f.base.RoundTrip(copy)
}
