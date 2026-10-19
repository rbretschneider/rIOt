package handlers

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// trimSnapshot preserves missing-section provenance. Readers may fall back to
// the last observation of a section, but never infer an empty or healthy value.
func trimSnapshot(data models.FullTelemetryData) models.FullTelemetryData {
	data.Trimmed = true
	data.DroppedSections = append([]string(nil), data.DroppedSections...)
	keep := map[string]bool{"system": true, "cpu": true, "memory": true, "disks": true, "network": true, "docker": true}
	v := reflect.ValueOf(&data).Elem()
	typ := v.Type()
	type field struct {
		index, size int
		name        string
		keep        bool
	}
	fields := []field{}
	for i := 0; i < v.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name == "trimmed" || name == "dropped_sections" {
			continue
		}
		raw, _ := json.Marshal(v.Field(i).Interface())
		fields = append(fields, field{i, len(raw), name, keep[name]})
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].keep != fields[j].keep {
			return !fields[i].keep
		}
		return fields[i].size > fields[j].size
	})
	for _, f := range fields {
		raw, _ := json.Marshal(data)
		if len(raw) <= models.TelemetryDownsampleMaxBytes {
			break
		}
		if f.size <= 4 {
			continue
		}
		v.Field(f.index).Set(reflect.Zero(v.Field(f.index).Type()))
		data.DroppedSections = append(data.DroppedSections, f.name)
	}
	return data
}
