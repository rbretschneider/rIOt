package handlers

import (
	"encoding/json"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"strings"
	"testing"
)

func TestDUX_SEC043_DownsampleCapWhenKeptSectionsTooLarge(t *testing.T) {
	d := models.FullTelemetryData{Docker: &models.DockerInfo{Available: true, Containers: []models.ContainerInfo{{Name: "web", Labels: map[string]string{"huge": strings.Repeat("x", 3<<20)}}}}}
	out := trimSnapshot(d)
	raw, _ := json.Marshal(out)
	if len(raw) > models.TelemetryDownsampleMaxBytes || !out.Trimmed || len(out.DroppedSections) == 0 {
		t.Fatalf("not capped: %d %+v", len(raw), out.DroppedSections)
	}
	if d.Docker == nil || len(d.Docker.Containers) != 1 {
		t.Fatal("mutated live telemetry")
	}
}
