package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
)

func isChatPOST(path string) bool {
	return path == "/chat/api/flow-action" || path == "/chat/api/prompt-answer" || path == "/chat/api/send" || path == "/chat/api/stop" || path == "/chat/api/new" || path == "/chat/api/mode" || path == "/chat/api/decide"
}
func (s *Server) chatAPI(w http.ResponseWriter, r *http.Request) {
	if isChatPOST(r.URL.Path) {
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", 405)
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
	if s.opts.Chat == nil && r.URL.Path != "/chat/api/flow-action" {
		http.Error(w, "Chat unavailable", 503)
		return
	}
	ctx := r.Context()
	var out any
	var err error
	switch r.URL.Path {
	case "/chat/api/flow-action":
		var in struct {
			Action    string `json:"action"`
			SessionID int64  `json:"session_id"`
			Machine   string `json:"machine"`
			Confirmed bool   `json:"confirmed"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&in)
		if err == nil {
			if s.opts.SessionAction == nil {
				err = errors.New("Local actions unavailable")
			} else if in.SessionID <= 0 || (in.Action != "jump" && in.Action != "kill") || in.Action == "kill" && !in.Confirmed {
				err = errors.New("Invalid action or missing confirmation")
			} else {
				snap, e := s.cachedLoad(ctx, store.Scope{}, false)
				err = e
				local := false
				for _, n := range snap.BoardData.Nodes {
					if n.ID == in.SessionID && n.MachineID == snap.LocalMachineID && in.Machine == n.MachineID {
						local = true
						break
					}
				}
				if err == nil && !local {
					err = errors.New("Select a local session")
				}
				if err == nil {
					err = s.opts.SessionAction(ctx, in.Action, in.SessionID)
				}
			}
		}
		out = map[string]bool{"ok": err == nil}
	case "/chat/api/prompts":
		if client, ok := s.opts.Chat.(chat.PromptClient); ok {
			out, err = client.Prompts(ctx)
		} else {
			out = []chat.Prompt{}
		}
	case "/chat/api/prompt-answer":
		var in struct {
			ID    string `json:"id"`
			Allow bool   `json:"allow"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&in)
		if err == nil {
			if client, ok := s.opts.Chat.(chat.PromptClient); ok {
				err = client.AnswerPrompt(ctx, in.ID, in.Allow)
			} else {
				err = errors.New("Prompts unavailable")
			}
		}
		out = map[string]bool{"ok": err == nil}
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
	case "/chat/api/wait":
		state, status, waitErr := s.chatWait(r)
		if waitErr != nil {
			http.Error(w, waitErr.Error(), status)
			return
		}
		out = renderChatState(state)
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

// chatWait answers one long poll: the whole conversation at once for a page
// that has none, otherwise the events past after, or the current status when
// pollTimeout passes without any.
func (s *Server) chatWait(r *http.Request) (chat.State, int, error) {
	var after uint64
	if raw := r.URL.Query().Get("after"); raw != "" {
		var err error
		if after, err = strconv.ParseUint(raw, 10, 64); err != nil {
			return chat.State{}, 400, errors.New("invalid replay cursor")
		}
	}
	id, ctx := r.URL.Query().Get("id"), r.Context()
	var state chat.State
	var err error
	if after == 0 {
		state, err = s.opts.Chat.Read(ctx, id, 0)
	} else {
		waitCtx, cancel := context.WithTimeout(ctx, s.pollTimeout)
		state, err = s.opts.Chat.Wait(waitCtx, id, after)
		// The client may give up first, on a deadline of its own: either
		// way nothing changed, and the poll answers with the current state.
		timedOut := ctx.Err() == nil && (waitCtx.Err() != nil || errors.Is(err, context.DeadlineExceeded))
		cancel()
		if err != nil && timedOut {
			state, err = s.opts.Chat.Read(ctx, id, after)
		}
	}
	if err != nil {
		return chat.State{}, 404, errors.New("conversation not found")
	}
	return state, 200, nil
}

// HTML belongs to the page, never persisted chat history or deltas.
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
	if m.Role == "assistant" && m.Status != "streaming" {
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
	for i, e := range state.Events {
		rendered := chatHTMLEvent{Event: e}
		if e.Message != nil {
			m := renderChatMessage(*e.Message)
			rendered.Message = &m
		}
		events[i] = rendered
	}
	return events
}
