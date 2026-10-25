package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/viewer"
)

// actionRequest is what a page button asks for. Machine names the machine
// the button's row is on; only the local machine's sessions, process trees,
// disk and projects are acted on.
type actionRequest struct {
	Action    string `json:"action"`
	SessionID int64  `json:"session_id,omitempty"`
	ID        string `json:"id,omitempty"`
	All       bool   `json:"all,omitempty"`
	Force     bool   `json:"force,omitempty"`
	Project   string `json:"project,omitempty"`
	Machine   string `json:"machine,omitempty"`
	// Confirmed is set once the app's dialog was answered, for the actions
	// that change something.
	Confirmed bool `json:"confirmed,omitempty"`
}

// appAction runs one of the local actions the TUI has keys for, through
// the same code Chat's tools use. Each is checked against the snapshot the
// page shows; the action itself checks again before it acts.
func (s *Server) appAction(w http.ResponseWriter, r *http.Request) {
	if s.opts.Action == nil {
		http.Error(w, "Local actions unavailable", http.StatusServiceUnavailable)
		return
	}
	var in actionRequest
	if !decodeAppJSON(w, r, &in) {
		return
	}
	snap, err := s.cachedLoad(r.Context(), store.Scope{}, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	args, err := actionArgs(in, snap)
	if err == nil {
		err = s.opts.Action(r.Context(), in.Action, args)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// actionArgs validates a request and returns the action's arguments.
func actionArgs(in actionRequest, snap viewer.Snapshot) (json.RawMessage, error) {
	local := in.Machine == snap.LocalMachineID
	// As in the TUI, acknowledging one item needs no confirmation.
	changes := in.Action == "kill" || in.Action == "cleanup" || in.Action == "ack_inbox" && in.All
	if changes && !in.Confirmed {
		return nil, errors.New("Confirm the action first")
	}
	var args any
	switch in.Action {
	case "jump", "kill":
		if in.Action == "kill" && in.ID != "" && in.SessionID == 0 {
			if !local || !localOrphan(snap, in.ID) {
				return nil, errors.New("Select a verified idle process tree on this machine")
			}
			args = map[string]any{"id": in.ID, "force": in.Force}
			break
		}
		if in.SessionID <= 0 || in.ID != "" || !local || !localSession(snap, in.SessionID) {
			return nil, errors.New("Select a local session")
		}
		if in.Action == "jump" {
			args = map[string]any{"session_id": in.SessionID}
		} else {
			args = map[string]any{"session_id": in.SessionID, "force": in.Force}
		}
	case "cleanup":
		if in.ID == "" || !local {
			return nil, errors.New("Select a cleanup candidate on this machine")
		}
		args = map[string]any{"id": in.ID}
	case "ack_inbox":
		// Acknowledgements reach other machines through the fleet.
		if in.All == (in.ID != "") {
			return nil, errors.New("Select one inbox item or all of them")
		}
		args = map[string]any{"id": in.ID, "all": in.All}
	case "summarize":
		if !local || !localProject(snap, in.Project) {
			return nil, errors.New("Select a project on this machine")
		}
		args = map[string]any{"project": in.Project}
	default:
		return nil, errors.New("Unknown action")
	}
	return json.Marshal(args)
}

func localSession(snap viewer.Snapshot, id int64) bool {
	for _, n := range snap.BoardData.Nodes {
		if n.ID == id && n.MachineID == snap.LocalMachineID {
			return true
		}
	}
	return false
}

// localOrphan reports whether id is a process tree on this machine that the
// TUI would offer to kill: verified idle and old enough.
func localOrphan(snap viewer.Snapshot, id string) bool {
	for _, o := range snap.Orphans.Rows {
		if o.ID == id && o.MachineID == snap.LocalMachineID {
			return o.Verified && !o.Young
		}
	}
	return false
}

func localProject(snap viewer.Snapshot, path string) bool {
	for _, p := range snap.Projects.Rows {
		if path != "" && p.Status.Repo.Path == path && p.MachineID == snap.LocalMachineID {
			return true
		}
	}
	return false
}
