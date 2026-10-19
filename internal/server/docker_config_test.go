package server

import "testing"

func TestDUX_AC108_RetentionParser(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want int
	}{{"", 7}, {"1", 1}, {"90", 90}, {"30", 30}, {"0", 7}, {"91", 7}, {"-1", 7}, {"abc", 7}, {" 7", 7}} {
		if got := parseContainerLogRetention(tt.raw); got != tt.want {
			t.Errorf("%q: got %d want %d", tt.raw, got, tt.want)
		}
	}
}
