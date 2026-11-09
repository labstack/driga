package reclaim

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

const MaxSimulatorInventoryBytes = 4 << 20

// SimulatorReview describes a booted device, without inferring who is using it.
// Boot time is an observation, not proof of idleness or permission to stop it.
type SimulatorReview struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Runtime      string     `json:"runtime"`
	State        string     `json:"state"`
	LastBootedAt *time.Time `json:"last_booted_at,omitempty"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	ObservedAt   time.Time  `json:"observed_at"`
	Status       string     `json:"status"`
	Reason       string     `json:"reason"`
}

// CollectSimulators performs one read-only inventory request. The caller owns
// the context deadline and the runner; this module never boots or stops devices.
func CollectSimulators(ctx context.Context, run func(context.Context, string, ...string) ([]byte, error), now time.Time) ([]SimulatorReview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if run == nil {
		return nil, errors.New("simulator inventory runner unavailable")
	}
	out, err := run(ctx, "xcrun", "simctl", "list", "devices", "booted", "--json")
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, errors.New("simulator inventory unavailable")
	}
	return parseSimulators(out, now)
}

func parseSimulators(data []byte, now time.Time) ([]SimulatorReview, error) {
	invalid := func() ([]SimulatorReview, error) {
		return nil, errors.New("invalid simulator inventory")
	}
	if len(data) > MaxSimulatorInventoryBytes {
		return invalid()
	}
	var inventory struct {
		Devices map[string][]struct {
			ID           string          `json:"udid"`
			Name         string          `json:"name"`
			State        string          `json:"state"`
			Available    *bool           `json:"isAvailable"`
			LastBootedAt json.RawMessage `json:"lastBootedAt"`
			LastUsedAt   json.RawMessage `json:"lastUsedAt"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(data, &inventory); err != nil || inventory.Devices == nil {
		return invalid()
	}
	rows := []SimulatorReview{}
	seen := map[string]bool{}
	for runtime, devices := range inventory.Devices {
		if runtime == "" || devices == nil {
			return invalid()
		}
		for _, device := range devices {
			if strings.TrimSpace(device.ID) == "" || device.State == "" {
				return invalid()
			}
			if device.State != "Booted" || device.Available != nil && !*device.Available {
				continue
			}
			if seen[device.ID] {
				return invalid()
			}
			seen[device.ID] = true
			row := SimulatorReview{
				ID: device.ID, Name: device.Name, Runtime: runtime, State: device.State,
				ObservedAt: now, Status: "needs_review",
				Reason: "Booted simulator; active use and session ownership are unknown. Review ongoing work before stopping it.",
			}
			var timestamp string
			if json.Unmarshal(device.LastBootedAt, &timestamp) == nil {
				if at, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
					row.LastBootedAt = &at
				}
			}
			if row.LastBootedAt == nil {
				row.Reason += " Last boot time unavailable."
			}
			// Last use is a distinct tool observation: it does not establish boot
			// time, current activity, or a safe shutdown opportunity.
			timestamp = ""
			if json.Unmarshal(device.LastUsedAt, &timestamp) == nil {
				if at, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
					row.LastUsedAt = &at
				}
			}
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, nil
}
