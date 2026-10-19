package models

import "time"

type DockerCommandStep struct {
	Seq        int        `json:"seq"`
	Name       string     `json:"name"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Outcome    string     `json:"outcome,omitempty"`
	Output     string     `json:"output,omitempty"`
	Truncated  bool       `json:"truncated,omitempty"`
}
type DockerTargetResult struct {
	Container string `json:"container"`
	Outcome   string `json:"outcome"`
	Message   string `json:"message"`
	Step      string `json:"step"`
}
