package handlers

import "github.com/DesyncTheThird/rIOt/internal/models"

// maxAutoUpdateTarget bounds the container name a list row offers as a policy
// target. The fleet projection cuts names at 128 characters, so a name of that
// length may be a truncation the dispatcher would never match; such rows get
// no target. The byte length is used, which only ever disables more rows.
const maxAutoUpdateTarget = 128

// FleetAutoUpdatePolicy is the auto-update state of one container row.
//
// Target is the telemetry name verbatim (the exact string checkAutoUpdates
// matches), or "" when no policy can be saved for the row. Enabled reports an
// enabled container policy under that name. CoveredBy is the Compose project
// whose enabled stack policy decides for this container instead.
type FleetAutoUpdatePolicy struct {
	Target    string `json:"target"`
	Enabled   bool   `json:"enabled"`
	CoveredBy string `json:"covered_by,omitempty"`
}

// autoUpdatePolicyIndex holds every policy by device, then by target. One
// target has at most one policy per device (the table's unique key).
type autoUpdatePolicyIndex map[string]map[string]models.AutoUpdatePolicy

func indexAutoUpdatePolicies(policies []models.AutoUpdatePolicy) autoUpdatePolicyIndex {
	ix := autoUpdatePolicyIndex{}
	for _, p := range policies {
		if ix[p.DeviceID] == nil {
			ix[p.DeviceID] = map[string]models.AutoUpdatePolicy{}
		}
		ix[p.DeviceID][p.Target] = p
	}
	return ix
}

// stackEnabled reports an enabled stack policy for project on the device.
func (ix autoUpdatePolicyIndex) stackEnabled(deviceID, project string) bool {
	p, ok := ix[deviceID][project]
	return ok && p.IsStack && p.Enabled
}

// container computes a container's policy state with the dispatcher's rule
// (checkAutoUpdates): a container with both the Compose project and
// working-dir labels is handled by an enabled stack policy for its project;
// otherwise an enabled container policy whose target equals the name applies.
// Policies of the other kind under the same name never count.
func (ix autoUpdatePolicyIndex) container(deviceID, name string, labels map[string]string) FleetAutoUpdatePolicy {
	var out FleetAutoUpdatePolicy
	if name != "" && len(name) < maxAutoUpdateTarget {
		out.Target = name
		p, ok := ix[deviceID][name]
		out.Enabled = ok && !p.IsStack && p.Enabled
	}
	project := labels[models.LabelComposeProject]
	workDir := labels[models.LabelComposeWorkingDir]
	if project != "" && workDir != "" && ix.stackEnabled(deviceID, project) {
		out.CoveredBy = project
	}
	return out
}

// containerOutOfDate is the Containers page's "Out of date" rule: the
// update_status verdict when the agent sends one, else the older true/false
// field. pinned and check_failed never count.
func containerOutOfDate(c models.ContainerInfo) bool {
	if c.UpdateStatus != "" {
		return c.UpdateStatus == models.UpdateStatusAvailable
	}
	return c.UpdateAvailable != nil && *c.UpdateAvailable
}
