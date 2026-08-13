package event

import "time"

// Event represents a unified system telemetry observation
type Event struct {
	Type         string            `json:"type"`                     // e.g. "foreground_session", "file_changed", "git_commit", "terminal_command"
	Source       string            `json:"source"`                   // e.g. "windowscollector", "filewatcher", "gitmonitor", "shellpipe"
	App          string            `json:"app,omitempty"`            // e.g. "chrome.exe", "Antigravity IDE.exe"
	Activity     string            `json:"activity,omitempty"`       // e.g. "foreground_interactive", "passive_media"
	Confidence   string            `json:"confidence,omitempty"`     // e.g. "high", "medium", "low"
	Time         time.Time         `json:"time"`                     // Event start time
	EndTime      time.Time         `json:"end_time,omitempty"`       // Event end time
	Duration     time.Duration     `json:"duration,omitempty"`       // Duration of activity
	HWND         uintptr           `json:"hwnd,omitempty"`           // Window handle (Windows only)
	PID          uint32            `json:"pid,omitempty"`            // Process ID
	Title        string            `json:"title,omitempty"`          // Window title / Commit message / Command text
	Category     string            `json:"category,omitempty"`       // "coding", "entertainment", "learning", "communication", "idle", "unknown"
	Project      string            `json:"project,omitempty"`        // Associated project name from goals.yaml (e.g. "PipelineForge")
	GoalRelevant int               `json:"goal_relevant"`            // 1 if directly relevant to goals/certs, 0 otherwise
	URL          string            `json:"url,omitempty"`            // Full URL (for browser events)
	Domain       string            `json:"domain,omitempty"`         // Website domain (for browser URLs)
	ExitCode     int               `json:"exit_code,omitempty"`      // For terminal commands
	Metadata     map[string]string `json:"metadata,omitempty"`       // Process name, path, custom payloads
}
