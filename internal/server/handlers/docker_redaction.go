package handlers

import (
	"encoding/json"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/redact"
)

// Redaction is repeated at reads so historical snapshots from older agents are safe.
// Every copy is independent: output masking must not mutate telemetry shared by
// command targeting, stack path agreement or a concurrent dashboard request.
func sanitizeDockerTelemetry(data *models.FullTelemetryData) *models.FullTelemetryData {
	if data == nil {
		return nil
	}
	out := *data
	if data.Docker != nil {
		d := *data.Docker
		d.Containers = make([]models.ContainerInfo, len(data.Docker.Containers))
		for i, c := range data.Docker.Containers {
			d.Containers[i] = sanitizeDockerContainer(c)
		}
		out.Docker = &d
	}
	return &out
}
func sanitizeDockerContainer(c models.ContainerInfo) models.ContainerInfo {
	c.Labels, _ = redact.Labels(c.Labels)
	c.Env = append([]models.EnvVar(nil), c.Env...)
	for i := range c.Env {
		c.Env[i].Value, _ = redact.EnvPair(c.Env[i].Key, c.Env[i].Value)
	}
	c.Image, _ = redact.MaskLine(c.Image)
	c.Status, _ = redact.MaskLine(c.Status)
	if c.Riot != nil {
		r := *c.Riot
		r.Name, _ = redact.MaskLine(r.Name)
		r.Description, _ = redact.MaskLine(r.Description)
		r.Icon, _ = redact.MaskLine(r.Icon)
		r.Group, _ = redact.MaskLine(r.Group)
		r.Tags = append([]string(nil), r.Tags...)
		for i := range r.Tags {
			r.Tags[i], _ = redact.MaskLine(r.Tags[i])
		}
		r.URL, _ = redact.URLCreds(r.URL)
		c.Riot = &r
	}
	return c
}
func sanitizeDockerOutput(v interface{}) interface{} {
	switch x := v.(type) {
	case *models.TelemetrySnapshot:
		if x == nil {
			return x
		}
		copy := *x
		copy.Data = *sanitizeDockerTelemetry(&x.Data)
		return &copy
	case []models.TelemetrySnapshot:
		out := append([]models.TelemetrySnapshot(nil), x...)
		for i := range out {
			out[i].Data = *sanitizeDockerTelemetry(&out[i].Data)
		}
		return out
	case *models.FullTelemetryData:
		return sanitizeDockerTelemetry(x)
	case models.ContainerInfo:
		return sanitizeDockerContainer(x)
	case []models.ContainerInfo:
		out := make([]models.ContainerInfo, len(x))
		for i, c := range x {
			out[i] = sanitizeDockerContainer(c)
		}
		return out
	case []models.ContainerLogEntry:
		out := append([]models.ContainerLogEntry(nil), x...)
		for i := range out {
			out[i].Line, _ = redact.MaskLine(out[i].Line)
		}
		return out
	case *models.Command:
		if x == nil {
			return x
		}
		c := sanitizeDockerCommand(*x)
		return &c
	case []models.Command:
		out := make([]models.Command, len(x))
		for i, c := range x {
			out[i] = sanitizeDockerCommand(c)
		}
		return out
	case []models.CommandOutput:
		out := append([]models.CommandOutput(nil), x...)
		for i := range out {
			out[i].Content, _ = redact.MaskText(out[i].Content, "<masked>")
		}
		return out
	case map[string]interface{}:
		out := map[string]interface{}{}
		for k, value := range x {
			out[k] = sanitizeDockerOutput(value)
		}
		return out
	}
	return v
}

func sanitizeDockerCommand(c models.Command) models.Command {
	c.ResultMsg, _ = redact.MaskText(c.ResultMsg, "<masked>")
	c.StderrTail, _ = redact.MaskText(c.StderrTail, "<masked>")
	c.ActorName, _ = redact.MaskLine(c.ActorName)
	c.Steps = append([]models.DockerCommandStep(nil), c.Steps...)
	for i := range c.Steps {
		c.Steps[i].Output, _ = redact.MaskText(c.Steps[i].Output, "<masked>")
	}
	c.TargetResults = append([]models.DockerTargetResult(nil), c.TargetResults...)
	if !c.State().IsTerminal() {
		c.TargetResults = nil
	}
	for i := range c.TargetResults {
		c.TargetResults[i].Message, _ = redact.MaskLine(c.TargetResults[i].Message)
	}
	if raw, e := json.Marshal(c.Params); e == nil {
		if safe, _, e := redact.InspectJSON(raw); e == nil {
			var params map[string]interface{}
			if json.Unmarshal(safe, &params) == nil {
				c.Params = params
			}
		}
	}
	return c
}
