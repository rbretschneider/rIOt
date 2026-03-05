package handlers

import (
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestExtractPrimaryIP(t *testing.T) {
	tests := []struct {
		name string
		data *models.FullTelemetryData
		want string
	}{
		{
			name: "nil network",
			data: &models.FullTelemetryData{Network: nil},
			want: "",
		},
		{
			name: "no interfaces",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{Interfaces: nil},
			},
			want: "",
		},
		{
			name: "loopback only",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "lo", State: "UP", IPv4: []string{"127.0.0.1/8"}},
					},
				},
			},
			want: "",
		},
		{
			name: "interface down",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "eth0", State: "DOWN", IPv4: []string{"192.168.1.10/24"}},
					},
				},
			},
			want: "",
		},
		{
			name: "CIDR stripping",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "eth0", State: "UP", IPv4: []string{"10.0.0.5/24"}},
					},
				},
			},
			want: "10.0.0.5",
		},
		{
			name: "bare IP without CIDR",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "eth0", State: "UP", IPv4: []string{"192.168.1.100"}},
					},
				},
			},
			want: "192.168.1.100",
		},
		{
			name: "first valid IP wins",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "lo", State: "UP", IPv4: []string{"127.0.0.1"}},
						{Name: "eth0", State: "UP", IPv4: []string{"10.0.0.1/24"}},
						{Name: "eth1", State: "UP", IPv4: []string{"172.16.0.1/16"}},
					},
				},
			},
			want: "10.0.0.1",
		},
		{
			name: "skip empty strings",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "eth0", State: "UP", IPv4: []string{"", "10.0.0.2/24"}},
					},
				},
			},
			want: "10.0.0.2",
		},
		{
			name: "skip 127.0.0.1 on non-lo interface",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "eth0", State: "UP", IPv4: []string{"127.0.0.1", "192.168.1.5"}},
					},
				},
			},
			want: "192.168.1.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPrimaryIP(tt.data)
			assert.Equal(t, tt.want, got)
		})
	}
}
