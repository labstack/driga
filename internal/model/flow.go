package model

import (
	"encoding/json"
	"time"

	"github.com/labstack/driga/internal/jsonwire"
)

// EventMetadata is an allowlist of OTLP names and numeric usage, never content.
// Pointers distinguish absent measurements from measured zero.
type EventMetadata struct {
	Model        string   `json:"model,omitempty"`
	Tool         string   `json:"tool,omitempty"`
	InputTokens  *int64   `json:"input_tokens,omitempty"`
	OutputTokens *int64   `json:"output_tokens,omitempty"`
	CostUSD      *float64 `json:"cost_usd,omitempty"`
}

// StateSignal explains an observation used by the state table. Strength is
// normalized freshness/availability, not a probability that the verdict is true.
type StateSignal struct {
	Name       string        `json:"name"`
	Reading    string        `json:"reading"`
	At         time.Time     `json:"observed_at"`
	Age        time.Duration `json:"age_ms"`
	Strength   float64       `json:"strength"`
	Supports   bool          `json:"supports"`
	Alive      *bool         `json:"alive,omitempty"`
	CPUPercent *float64      `json:"cpu_percent,omitempty"`
}

// StateInput contains the observations consumed by the label table.
type StateInput struct {
	LastHook         string     `json:"last_hook"`
	LastHookAt       time.Time  `json:"last_hook_at"`
	Obs              SessionObs `json:"obs"`
	Proc             *Proc      `json:"proc"`
	LastNotification *time.Time `json:"last_notification"`
	LastStop         *time.Time `json:"last_stop"`
	LastUserPrompt   time.Time  `json:"last_user_prompt"`
	Unseen           bool       `json:"unseen"`
	LastDecision     *time.Time `json:"last_decision"`
	LastAPIRequest   *time.Time `json:"last_api_request"`
	LastToolResult   *time.Time `json:"last_tool_result"`
	LastAPIError     string     `json:"last_api_error"`
}

// StateMixer is saved atomically with its verdict and the label-pass inputs.
type StateMixer struct {
	Input          *StateInput   `json:"input,omitempty"`
	StalledAfterMS int64         `json:"stalled_after_ms,omitempty"`
	Verdict        string        `json:"verdict"`
	Heuristic      bool          `json:"heuristic"`
	Evidence       string        `json:"evidence"`
	Agreement      int           `json:"agreement"`
	ObservedAt     time.Time     `json:"observed_at"`
	Rows           []StateSignal `json:"rows"`
}

func (v StateMixer) MarshalJSON() ([]byte, error)  { return jsonwire.Marshal(v) }
func (v StateSignal) MarshalJSON() ([]byte, error) { return jsonwire.Marshal(v) }
func (v *StateSignal) UnmarshalJSON(b []byte) error {
	type raw StateSignal
	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	r.Age *= time.Millisecond
	*v = StateSignal(r)
	return nil
}
