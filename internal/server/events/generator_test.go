package events

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareValue(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		operator  string
		threshold float64
		want      bool
	}{
		{"gt true", 91, ">", 90, true},
		{"gt false", 89, ">", 90, false},
		{"gt equal", 90, ">", 90, false},
		{"lt true", 5, "<", 10, true},
		{"lt false", 15, "<", 10, false},
		{"gte true equal", 90, ">=", 90, true},
		{"gte true above", 91, ">=", 90, true},
		{"gte false", 89, ">=", 90, false},
		{"lte true equal", 10, "<=", 10, true},
		{"lte true below", 5, "<=", 10, true},
		{"lte false", 15, "<=", 10, false},
		{"eq true", 42, "==", 42, true},
		{"eq false", 42, "==", 43, false},
		{"neq true", 42, "!=", 43, true},
		{"neq false", 42, "!=", 42, false},
		{"invalid operator", 1, "~", 1, false},
		{"empty operator", 1, "", 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareValue(tt.value, tt.operator, tt.threshold)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchesDeviceFilter(t *testing.T) {
	tests := []struct {
		name     string
		filter   string
		deviceID string
		want     bool
	}{
		{"empty filter matches all", "", "device-1", true},
		{"exact match", "device-1", "device-1", true},
		{"no match", "device-2", "device-1", false},
		{"comma separated match first", "device-1,device-2", "device-1", true},
		{"comma separated match second", "device-1,device-2", "device-2", true},
		{"comma separated no match", "device-1,device-2", "device-3", false},
		{"whitespace trimmed", "device-1, device-2 , device-3", "device-2", true},
		{"partial id no match", "device-1", "device-10", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesDeviceFilter(tt.filter, tt.deviceID)
			assert.Equal(t, tt.want, got)
		})
	}
}
