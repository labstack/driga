package model

import "time"

type MCPHost struct {
	PID       int32         `json:"pid"`
	Name      string        `json:"name"`
	Exe       string        `json:"exe"`
	Cwd       string        `json:"cwd"`
	StartedAt time.Time     `json:"started_at"`
	Age       time.Duration `json:"age_ns"`
}

// ProcessIdentity distinguishes PID reuse. Launcher processes are excluded:
// membership represents logical servers, while Footprint includes launchers.
type ProcessIdentity struct {
	PID       int32     `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// MCPObservation is the first successful process observation in a minute. At retains
// the actual sample time rather than implying a longer observation window.
type MCPObservation struct {
	ResolutionSeconds int // 60 for full detail; 3600 for hourly retained endpoints.
	At                time.Time
	Host              MCPHost
	Key, Name         string
	Count             int
	Footprint         uint64
	Members           []ProcessIdentity
}
