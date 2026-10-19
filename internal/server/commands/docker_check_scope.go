package commands

import (
	"encoding/json"
	"regexp"
)

var checkComposeProject = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)
var checkContainerID = regexp.MustCompile(`^[a-f0-9]{12,64}$`)

// Empty params retains the existing host-wide operation. Explicit selections
// must be nonempty and bounded, so malformed scope never becomes host-wide.
func validateDockerCheckParams(params map[string]interface{}) error {
	for key := range params {
		if key != "container_ids" && key != "compose_project" {
			return ErrInvalidCommandParams
		}
	}
	if raw, ok := params["compose_project"]; ok {
		project, ok := raw.(string)
		if !ok || !checkComposeProject.MatchString(project) {
			return ErrInvalidCommandParams
		}
	}
	if raw, ok := params["container_ids"]; ok {
		data, err := json.Marshal(raw)
		if err != nil {
			return ErrInvalidCommandParams
		}
		var ids []string
		if json.Unmarshal(data, &ids) != nil || len(ids) == 0 || len(ids) > 500 {
			return ErrInvalidCommandParams
		}
		for _, id := range ids {
			if !checkContainerID.MatchString(id) {
				return ErrInvalidCommandParams
			}
		}
	}
	return nil
}
