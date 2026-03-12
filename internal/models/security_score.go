package models

import "time"

// SecuritySeverity indicates the importance of a security finding.
type SecuritySeverity string

const (
	SecSeverityCritical SecuritySeverity = "critical"
	SecSeverityWarning  SecuritySeverity = "warning"
	SecSeverityInfo     SecuritySeverity = "info"
	SecSeverityPass     SecuritySeverity = "pass"
)

// SecurityCategory groups related security checks.
type SecurityCategory string

const (
	CategoryAccessControl SecurityCategory = "access_control"
	CategoryPatching      SecurityCategory = "patching"
	CategoryNetwork       SecurityCategory = "network"
	CategoryDocker        SecurityCategory = "docker"
	CategorySystem        SecurityCategory = "system"
)

// SecurityFinding is the result of a single security check.
type SecurityFinding struct {
	ID          string           `json:"id"`
	Category    SecurityCategory `json:"category"`
	Severity    SecuritySeverity `json:"severity"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Remediation string           `json:"remediation"`
	Weight      int              `json:"weight"`
	Passed      bool             `json:"passed"`
}

// SecurityCategoryScore is the aggregated score for one category.
type SecurityCategoryScore struct {
	Category SecurityCategory  `json:"category"`
	Label    string            `json:"label"`
	Score    int               `json:"score"`
	MaxScore int               `json:"max_score"`
	Findings []SecurityFinding `json:"findings"`
}

// SecurityScore is the full security evaluation for a device.
type SecurityScore struct {
	OverallScore int                     `json:"overall_score"`
	MaxScore     int                     `json:"max_score"`
	Grade        string                  `json:"grade"`
	Categories   []SecurityCategoryScore `json:"categories"`
	EvaluatedAt  time.Time               `json:"evaluated_at"`
}
