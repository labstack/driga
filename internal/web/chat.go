package web

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
)

func isChatPOST(path string) bool {
	return path == "/chat/api/send" || path == "/chat/api/stop" || path == "/chat/api/new" || path == "/chat/api/mode" || path == "/chat/api/decide"
}
func (s *Server) chatAPI(w http.ResponseWriter, r *http.Request) {
	if isChatPOST(r.URL.Path) {
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", 405)
			return
		}
		if s.origin.Check(r) != nil || r.Header.Get("Origin") != "http://"+r.Host || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.csrf)) != 1 {
			http.Error(w, "Origin or CSRF token rejected", 403)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "expected application/json", 415)
			return
		}
	} else if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if s.opts.Chat == nil {
		http.Error(w, "Chat unavailable", 503)
		return
	}
	ctx := r.Context()
	var out any
	var err error
	switch r.URL.Path {
	case "/chat/api/new":
		out, err = s.opts.Chat.Create(ctx)
	case "/chat/api/history":
		out, err = s.opts.Chat.List(ctx)
	case "/chat/api/send", "/chat/api/stop":
		var in struct {
			ID      string       `json:"id"`
			Text    string       `json:"text"`
			Context chat.Context `json:"context"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, chat.MaxText+8192))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&in)
		if err == nil {
			if r.URL.Path == "/chat/api/stop" {
				err = s.opts.Chat.Stop(ctx, in.ID)
			} else {
				err = s.opts.Chat.Send(ctx, in.ID, in.Text, in.Context)
			}
		}
		out = map[string]bool{"ok": err == nil}
	case "/chat/api/mode", "/chat/api/decide":
		var in struct {
			ID     string    `json:"id"`
			Mode   chat.Mode `json:"mode"`
			Action string    `json:"action"`
			Allow  bool      `json:"allow"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&in)
		if err == nil {
			if r.URL.Path == "/chat/api/mode" {
				err = s.opts.Chat.SetMode(ctx, in.ID, in.Mode)
			} else {
				err = s.opts.Chat.Decide(ctx, in.ID, in.Action, in.Allow)
			}
		}
		out = map[string]bool{"ok": err == nil}
	case "/chat/api/events":
		s.chatEvents(w, r)
		return
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != "HEAD" {
		_ = json.NewEncoder(w).Encode(out)
	}
}
func (s *Server) chatEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method == "HEAD" {
		w.Header().Set("Content-Type", "text/event-stream")
		return
	}
	select {
	case s.chatStreams <- struct{}{}:
		defer func() { <-s.chatStreams }()
	default:
		http.Error(w, "too many Chat streams", 429)
		return
	}
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("after")
	}
	var after uint64
	var err error
	if raw != "" {
		after, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			http.Error(w, "invalid replay cursor", 400)
			return
		}
	}
	state, err := s.opts.Chat.Read(r.Context(), r.URL.Query().Get("id"), after)
	if err != nil {
		http.Error(w, "conversation not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	write := func(id uint64, kind string, data any) error {
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Now().Add(s.sseWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", id, kind, raw); err != nil {
			return err
		}
		return rc.Flush()
	}
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	var active bool
	queued := -1
	for {
		if state.Reset {
			if err := write(state.Cursor, "reset", renderChatState(state)); err != nil {
				return
			}
		} else {
			for _, e := range renderChatEvents(state) {
				if err := write(e.Seq, "change", e); err != nil {
					return
				}
			}
		}
		after = state.Cursor
		if state.Active != active || state.Queued != queued {
			active, queued = state.Active, state.Queued
			if err := write(after, "status", map[string]any{"active": active, "queued": queued}); err != nil {
				return
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if err := write(after, "status", map[string]any{"active": active, "queued": queued}); err != nil {
				return
			}
		case <-tick.C:
		}
		state, err = s.opts.Chat.Read(r.Context(), r.URL.Query().Get("id"), after)
		if err != nil {
			return
		}
	}
}

// HTML belongs to the browser transport, never persisted chat history or deltas.
type chatHTMLMessage struct {
	store.ChatMessage
	HTML string `json:"html,omitempty"`
}
type chatHTMLEvent struct {
	chat.Event
	Message *chatHTMLMessage `json:"message,omitempty"`
	HTML    string           `json:"html,omitempty"`
}
type chatHTMLState struct {
	chat.State
	Messages []chatHTMLMessage `json:"messages"`
	Events   []chatHTMLEvent   `json:"events"`
}

func renderChatMessage(m store.ChatMessage) chatHTMLMessage {
	out := chatHTMLMessage{ChatMessage: m}
	if m.Role == "assistant" {
		out.HTML = chat.MarkdownHTML(m.Text)
	}
	return out
}
func renderChatState(state chat.State) chatHTMLState {
	out := chatHTMLState{State: state, Messages: []chatHTMLMessage{}, Events: make([]chatHTMLEvent, len(state.Events))}
	for _, m := range state.Messages {
		out.Messages = append(out.Messages, renderChatMessage(m))
	}
	out.Events = renderChatEvents(state)
	return out
}

func renderChatEvents(state chat.State) []chatHTMLEvent {
	events := make([]chatHTMLEvent, len(state.Events))
	if len(events) == 0 {
		return events
	}
	texts := map[string]string{}
	for _, m := range state.Messages {
		if m.Role == "assistant" {
			texts[m.ID] = m.Text
		}
	}
	// Read supplies the current full messages plus ordered replay events. Rewind
	// their deltas so every replay HTML field reflects precisely that update.
	for i := len(state.Events) - 1; i >= 0; i-- {
		e := state.Events[i]
		rendered := chatHTMLEvent{Event: e}
		if e.Message != nil {
			m := renderChatMessage(*e.Message)
			rendered.Message = &m
			if m.Role == "assistant" {
				texts[m.ID] = m.Text
			}
		}
		if e.Kind == "delta" {
			text := texts[e.MessageID]
			rendered.HTML = chat.MarkdownHTML(text)
			texts[e.MessageID] = strings.TrimSuffix(text, e.Text)
		}
		events[i] = rendered
	}
	return events
}
