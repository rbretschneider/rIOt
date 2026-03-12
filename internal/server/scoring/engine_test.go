package scoring

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func TestScore_NilData(t *testing.T) {
	result := Score(&models.FullTelemetryData{})
	assert.Equal(t, 0, result.OverallScore)
	assert.Equal(t, "F", result.Grade)
	assert.Empty(t, result.Categories)
}

func TestScore_PerfectSecurity(t *testing.T) {
	boolTrue := true
	data := &models.FullTelemetryData{
		Security: &models.SecurityInfo{
			FirewallStatus:  "active",
			AppArmor:        "enabled",
			FailedLogins24h: 0,
			LoggedInUsers:   1,
			OpenPorts:       []int{22, 443},
		},
		Updates: &models.UpdateInfo{
			PendingUpdates:       0,
			PendingSecurityCount: 0,
			PendingKernelUpdate:  false,
			UnattendedUpgrades:   true,
		},
		Services: []models.ServiceInfo{
			{Name: "nginx", State: "running"},
			{Name: "sshd", State: "running"},
		},
		OS: &models.OSInfo{
			Uptime: 7 * 86400, // 7 days
		},
		Network: &models.NetworkInfo{
			DNSServers: []string{"10.0.0.1"},
		},
		WebServers: &models.WebServerInfo{
			Servers: []models.ProxyServer{{
				ConfigValid: &boolTrue,
				SecurityConfig: &models.ProxySecurityCfg{
					SecurityHeaders: map[string]string{
						"Strict-Transport-Security": "max-age=31536000",
						"X-Frame-Options":           "DENY",
						"X-Content-Type-Options":    "nosniff",
					},
					RateLimiting: []models.RateLimitRule{{Zone: "api", Rate: "10r/s"}},
				},
			}},
		},
	}

	result := Score(data)
	assert.Equal(t, 100, result.OverallScore)
	assert.Equal(t, "A", result.Grade)

	// All findings should pass
	for _, cat := range result.Categories {
		for _, f := range cat.Findings {
			assert.True(t, f.Passed, "expected %s to pass", f.ID)
		}
	}
}

func TestScore_PoorSecurity(t *testing.T) {
	boolFalse := false
	data := &models.FullTelemetryData{
		Security: &models.SecurityInfo{
			FirewallStatus:  "inactive",
			FailedLogins24h: 100,
			LoggedInUsers:   5,
			OpenPorts:       []int{21, 22, 23, 25, 80, 443, 514, 3306, 5432, 8080},
		},
		Updates: &models.UpdateInfo{
			PendingUpdates:       20,
			PendingSecurityCount: 5,
			PendingKernelUpdate:  true,
			UnattendedUpgrades:   false,
		},
		WebServers: &models.WebServerInfo{
			Servers: []models.ProxyServer{{
				ConfigValid: &boolFalse,
				Certs: []models.ProxyCert{{
					Subject:  "expired.example.com",
					DaysLeft: -5,
				}},
			}},
		},
	}

	result := Score(data)
	assert.Less(t, result.OverallScore, 30)
	assert.Equal(t, "F", result.Grade)
}

func TestScore_DockerCategory(t *testing.T) {
	data := &models.FullTelemetryData{
		Docker: &models.DockerInfo{
			Available: true,
			Containers: []models.ContainerInfo{
				{
					Name:          "app",
					State:         "running",
					RestartPolicy: "unless-stopped",
					HealthStatus:  "healthy",
					MemLimit:      536870912,
				},
			},
		},
	}

	result := Score(data)
	require.Len(t, result.Categories, 1)
	assert.Equal(t, models.CategoryDocker, result.Categories[0].Category)
	assert.Equal(t, result.Categories[0].MaxScore, result.Categories[0].Score)
}

func TestScore_DockerSkippedWhenUnavailable(t *testing.T) {
	data := &models.FullTelemetryData{
		Docker: &models.DockerInfo{Available: false},
	}
	result := Score(data)
	for _, cat := range result.Categories {
		assert.NotEqual(t, models.CategoryDocker, cat.Category)
	}
}

func TestScore_DockerSensitiveMounts(t *testing.T) {
	data := &models.FullTelemetryData{
		Docker: &models.DockerInfo{
			Available: true,
			Containers: []models.ContainerInfo{
				{
					Name:          "portainer",
					State:         "running",
					RestartPolicy: "always",
					HealthStatus:  "healthy",
					MemLimit:      268435456,
					Mounts: []models.MountInfo{
						{Type: "bind", Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock", ReadOnly: false},
					},
				},
			},
		},
	}

	result := Score(data)
	require.Len(t, result.Categories, 1)
	var found *models.SecurityFinding
	for i, f := range result.Categories[0].Findings {
		if f.ID == "no-sensitive-mounts" {
			found = &result.Categories[0].Findings[i]
			break
		}
	}
	require.NotNil(t, found)
	assert.False(t, found.Passed)
}

func TestGrade(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		{100, "A"}, {90, "A"}, {89, "B"}, {75, "B"},
		{74, "C"}, {60, "C"}, {59, "D"}, {40, "D"},
		{39, "F"}, {0, "F"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, grade(tt.score), "score=%d", tt.score)
	}
}
