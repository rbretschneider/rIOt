package events

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// [AC-020] auth_failure template must be present with all required field values.
func TestAC020_AuthFailureTemplate_PresentWithCorrectFields(t *testing.T) {
	templates := AlertTemplates()

	var tpl *models.AlertTemplate
	for i := range templates {
		if templates[i].ID == "auth_failure" {
			tpl = &templates[i]
			break
		}
	}

	require.NotNil(t, tpl, "[AC-020] auth_failure template must be present in AlertTemplates()")
	assert.Equal(t, "Auth Failure", tpl.Name, "[AC-020] template name must be 'Auth Failure'")
	assert.Equal(t, "security", tpl.Category, "[AC-020] template category must be 'security'")
	assert.Equal(t, "failed_logins_interval", tpl.Metric, "[AC-020] template metric must be 'failed_logins_interval'")
	assert.Equal(t, ">", tpl.Operator, "[AC-020] template operator must be '>'")
	assert.Equal(t, float64(0), tpl.Threshold, "[AC-020] template default threshold must be 0")
	assert.Equal(t, "warning", tpl.Severity, "[AC-020] template default severity must be 'warning'")
	assert.Equal(t, 300, tpl.CooldownSeconds, "[AC-020] template default cooldown must be 300 seconds")
	assert.False(t, tpl.NeedsTargetName, "[AC-020] NeedsTargetName must be false")
}

// [AC-020] auth_failure template Description must contain the SEC-004 operator warning.
func TestAC020_AuthFailureTemplate_DescriptionContainsSEC004Warning(t *testing.T) {
	templates := AlertTemplates()

	var tpl *models.AlertTemplate
	for i := range templates {
		if templates[i].ID == "auth_failure" {
			tpl = &templates[i]
			break
		}
	}

	require.NotNil(t, tpl)
	assert.True(t,
		strings.Contains(tpl.Description, "internet-facing SSH hosts"),
		"[AC-020/SEC-004] Description must warn about internet-facing SSH hosts: got %q", tpl.Description)
}

// [AC-023] Category "security" must appear in the template list.
func TestAC023_SecurityCategory_PresentInTemplateList(t *testing.T) {
	templates := AlertTemplates()

	categories := make(map[string]bool)
	for _, tpl := range templates {
		categories[tpl.Category] = true
	}

	assert.True(t, categories["security"],
		"[AC-023] category 'security' must appear in at least one template")
}

// [AC-023] auth_failure template appears under the "security" category.
func TestAC023_AuthFailureTemplate_UnderSecurityCategory(t *testing.T) {
	templates := AlertTemplates()

	var found bool
	for _, tpl := range templates {
		if tpl.Category == "security" && tpl.ID == "auth_failure" {
			found = true
			break
		}
	}

	assert.True(t, found,
		"[AC-023] auth_failure template must appear under category 'security'")
}

// [AC-030] log_errors template must be present in the template list.
func TestAC030_LogErrorsTemplate_PresentInTemplateList(t *testing.T) {
	templates := AlertTemplates()

	var tpl *models.AlertTemplate
	for i := range templates {
		if templates[i].ID == "log_errors" {
			tpl = &templates[i]
			break
		}
	}

	require.NotNil(t, tpl, "[AC-030] log_errors template must be present in AlertTemplates()")
	assert.Equal(t, "Log Errors Detected", tpl.Name, "[AC-030] log_errors display name must be 'Log Errors Detected'")
	assert.Equal(t, "system", tpl.Category, "[AC-030] log_errors must be in 'system' category (picker groups by category)")
}
