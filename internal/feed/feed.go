// Package feed encodes the natural-key, privacy-filtered fleet change stream.
package feed

import (
	"bytes"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
)

// Kind identifies a fleet payload.
type Kind string

// FeedRow is one committed change, ordered by its persistent sequence.
type FeedRow struct {
	Seq     int64           `json:"seq"`
	TS      time.Time       `json:"ts"`
	Kind    string          `json:"kind"`
	Key     string          `json:"key"`
	Payload json.RawMessage `json:"payload"`
}

const (
	Machine       Kind = "machine"
	Session       Kind = "session"
	SessionFiles  Kind = "session_files"
	Event         Kind = "event"
	Handoff       Kind = "handoff"
	Summary       Kind = "summary"
	Inbox         Kind = "inbox"
	Repo          Kind = "repo"
	Worktree      Kind = "worktree"
	Orphan        Kind = "orphan"
	Procs         Kind = "procs"
	MachineSample Kind = "machine_sample"
	Source        Kind = "source"
	Gap           Kind = "gap"
	Delete        Kind = "delete"
	Resnapshot    Kind = "resnapshot"
)

// ShareText controls which text is published.
type ShareText string

const (
	None     ShareText = "none"
	Titles   ShareText = "titles"
	Snippets ShareText = "snippets"
)

// EffectiveShare enforces the local capture opt-in as well as the sharing gate.
func EffectiveShare(share ShareText, capture string) ShareText {
	if share == None {
		return None
	}
	if share == Snippets && capture == "snippets" {
		return Snippets
	}
	return Titles
}

// SessionKey identifies a session without a database integer ID.
type SessionKey struct {
	Adapter    string `json:"adapter"`
	ExternalID string `json:"external_id"`
}

// Key encodes an unambiguous composite natural key as a JSON array.
func Key(parts ...any) string { b, _ := json.Marshal(parts); return string(b) }

// String returns the session's composite natural key.
func (s SessionKey) String() string { return Key(s.Adapter, s.ExternalID) }

// SessionRow supplies natural references for a stored session.
type SessionRow struct {
	model.Session
	Parent       *SessionKey `json:"parent,omitempty"`
	RepoPath     string      `json:"repo_path,omitempty"`
	WorktreePath string      `json:"worktree_path,omitempty"`
}

// FilesRow replaces a session's recent file-touch window.
type FilesRow struct {
	MachineID string            `json:"machine_id"`
	Session   SessionKey        `json:"session"`
	Files     []model.FileTouch `json:"files"`
}

// HandoffRow attributes a completion to its natural session key.
type HandoffRow struct {
	MachineID string     `json:"machine_id"`
	Session   SessionKey `json:"session"`
	TS        time.Time  `json:"ts"`
	Text      string     `json:"text"`
}

// InboxRow supplies a natural session reference for an attention item.
type InboxRow struct {
	model.InboxItem
	Session SessionKey `json:"session"`
}

// WorktreeRow supplies the repository path for a checkout.
type WorktreeRow struct {
	model.Worktree
	RepoPath string `json:"repo_path"`
}

type SummaryRow struct {
	model.Summary
	RepoPath string `json:"repo_path"`
}

// ProcsRow is the agent-only replacement process snapshot.
type ProcsRow struct {
	MachineID string               `json:"machine_id"`
	Processes []model.Proc         `json:"processes"`
	Sessions  map[int64]SessionKey `json:"-"`
}

// DeleteRow removes an entity by kind and natural key.
type DeleteRow struct {
	Kind Kind   `json:"kind"`
	Key  string `json:"key"`
}

// ResnapshotRow explains why a cursor cannot be resumed.
type ResnapshotRow struct {
	Reason string `json:"reason"`
}

// StripURLCredentials removes URL userinfo, including percent-encoded passwords.
func StripURLCredentials(raw string) string {
	// Git also accepts scp-style SSH remotes, which are not URLs.
	if !strings.Contains(raw, "://") {
		return scrub.Scrub(raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	return scrub.Scrub(u.String())
}

func object(row any) map[string]any {
	b, err := json.Marshal(row)
	if err != nil {
		return nil
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if decoder.Decode(&result) != nil {
		return nil
	}
	return result
}

// metadataCommand keeps the executable and flag names, never positional or
// flag values. Quotes keep a prompt containing whitespace in a single argument.
func metadataCommand(command string) string {
	var args []string
	var token strings.Builder
	var quote rune
	escaped := false
	for _, r := range command {
		switch {
		case escaped:
			token.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				token.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r) || r == 0:
			if token.Len() > 0 {
				args = append(args, token.String())
				token.Reset()
			}
		default:
			token.WriteRune(r)
		}
	}
	if token.Len() > 0 {
		args = append(args, token.String())
	}
	if len(args) == 0 {
		return ""
	}
	result := []string{filepath.Base(args[0])}
	for _, arg := range args[1:] {
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "-") {
			flag, _, _ := strings.Cut(arg, "=")
			// Short options can carry an attached value (-pPROMPT). Without
			// an option schema, conservatively retain only the first letter.
			if !strings.HasPrefix(flag, "--") && len(flag) > 2 {
				flag = flag[:2]
			}
			if strings.ContainsFunc(flag, unicode.IsSpace) {
				continue
			}
			result = append(result, flag)
		}
	}
	return strings.Join(result, " ")
}

// Encode returns the natural key and payload. A nil payload withholds a handoff
// when snippets are disabled, or indicates an unsupported/invalid row. Callers
// must supply the *Row wrappers above to resolve integer foreign references.
func Encode(kind Kind, row any, share ShareText) (key string, payload []byte) {
	var data any = row
	switch kind {
	case Summary:
		s, ok := row.(SummaryRow)
		if !ok || share != Titles && share != Snippets || s.UsedSnippets && share != Snippets {
			return "", nil
		}
		key = Key(s.RepoPath, s.UsedSnippets)
		s.Text = scrub.Snippet(s.Text)
		data = s
	case Machine:
		m, ok := row.(model.Machine)
		if !ok {
			return "", nil
		}
		key = m.MachineID
		if key == "" {
			key = m.ID
		}
		data = map[string]any{"machine_id": key, "hostname": m.Hostname, "os": m.OS, "arch": m.Arch, "cores": m.Cores, "mem_total": m.MemTotal, "driga_version": m.DrigaVersion}
	case Session:
		var s SessionRow
		switch v := row.(type) {
		case model.Session:
			s.Session = v
		case SessionRow:
			s = v
		default:
			return "", nil
		}
		key = (SessionKey{s.Adapter, s.ExternalID}).String()
		s.Completions, s.FilesTouched = nil, nil
		s.Title = scrub.Scrub(s.Title)
		if share == None {
			s.Title = ""
		}
		if share != Snippets {
			s.LastPrompt = ""
		} else {
			s.LastPrompt = scrub.Snippet(s.LastPrompt)
		}
		data = s
	case SessionFiles:
		f, ok := row.(FilesRow)
		if !ok {
			return "", nil
		}
		key = f.Session.String()
	case Event:
		e, ok := row.(model.Event)
		if !ok {
			return "", nil
		}
		key = Key(e.Adapter, e.ExternalID, e.TS.UTC().Format(time.RFC3339Nano), e.Kind)
		if share != Snippets {
			e.Summary = ""
		} else {
			e.Summary = scrub.Snippet(e.Summary)
		}
		data = e
	case Handoff:
		if share != Snippets {
			return "", nil
		}
		h, ok := row.(HandoffRow)
		if !ok {
			return "", nil
		}
		key = Key(h.Session.Adapter, h.Session.ExternalID, h.TS.UTC().Format(time.RFC3339Nano))
		h.Text = scrub.Snippet(h.Text)
		data = h
	case Inbox:
		var i InboxRow
		switch v := row.(type) {
		case model.InboxItem:
			i.InboxItem = v
		case InboxRow:
			i = v
		default:
			return "", nil
		}
		key = i.ID
		if share != Snippets {
			i.Summary = "turn finished"
			if i.Kind == "waiting" {
				i.Summary = "waiting for input"
			}
		} else {
			i.Summary = scrub.Snippet(i.Summary)
		}
		data = i
	case Repo:
		r, ok := row.(model.Repo)
		if !ok {
			return "", nil
		}
		key = r.Path
		r.RemoteURL = StripURLCredentials(r.RemoteURL)
		data = r
	case Worktree:
		var w WorktreeRow
		switch v := row.(type) {
		case model.Worktree:
			w.Worktree = v
		case WorktreeRow:
			w = v
		default:
			return "", nil
		}
		key = w.Path
		if share == None {
			w.LastCommitSubject = ""
		} else {
			w.LastCommitSubject = scrub.Scrub(w.LastCommitSubject)
		}
		data = w
	case Orphan:
		o, ok := row.(model.Orphan)
		if !ok {
			return "", nil
		}
		key = o.ID
	case Procs:
		p, ok := row.(ProcsRow)
		if !ok {
			return "", nil
		}
		key = p.MachineID
		processes := make([]map[string]any, 0, len(p.Processes))
		for _, proc := range p.Processes {
			if proc.Agent == "" {
				continue
			}
			if share != Snippets {
				proc.Cmdline = metadataCommand(proc.Cmdline)
			}
			proc.Cmdline = scrub.Snippet(proc.Cmdline)
			v := object(proc)
			delete(v, "session_id")
			if s, ok := p.Sessions[proc.SessionID]; ok {
				v["session"] = s
			}
			processes = append(processes, v)
		}
		data = map[string]any{"machine_id": p.MachineID, "processes": processes}
	case MachineSample:
		s, ok := row.(model.MachineSample)
		if !ok {
			return "", nil
		}
		key = s.TS.UTC().Format(time.RFC3339Nano)
	case Source:
		s, ok := row.(model.SourceStatus)
		if !ok {
			return "", nil
		}
		key = s.Name
		s.LastError = scrub.Scrub(s.LastError)
		data = s
	case Gap:
		g, ok := row.(model.Gap)
		if !ok {
			return "", nil
		}
		key = Key(g.From.UTC().Format(time.RFC3339Nano), g.To.UTC().Format(time.RFC3339Nano))
	case Delete:
		d, ok := row.(DeleteRow)
		if !ok {
			return "", nil
		}
		key = Key(d.Kind, d.Key)
	case Resnapshot:
		key = "resnapshot"
	default:
		return "", nil
	}
	v := object(data)
	if v == nil {
		return "", nil
	}
	// IDs used by inbox/orphans are already natural strings; other database IDs
	// and every integer foreign key are private to the originating SQLite DB.
	if kind != Inbox && kind != Orphan {
		delete(v, "id")
	}
	for _, field := range []string{"parent_id", "parent_session_id", "repo_id", "worktree_id", "session_id", "project_id"} {
		delete(v, field)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", nil
	}
	return key, b
}
