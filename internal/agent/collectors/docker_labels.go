package collectors

import (
	"strconv"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

const labelPrefix = "riot."

// ParseRiotLabels extracts riot.* labels from a container label map.
func ParseRiotLabels(labels map[string]string) *models.RiotLabels {
	rl := &models.RiotLabels{
		Priority: 50, // default mid-priority
	}

	found := false
	for k, v := range labels {
		if !strings.HasPrefix(k, labelPrefix) {
			continue
		}
		found = true
		key := strings.TrimPrefix(k, labelPrefix)
		switch key {
		case "group":
			rl.Group = v
		case "name":
			rl.Name = v
		case "icon":
			rl.Icon = v
		case "description":
			rl.Description = v
		case "url":
			rl.URL = v
		case "priority":
			if n, err := strconv.Atoi(v); err == nil {
				rl.Priority = n
			}
		case "hide":
			rl.Hide = v == "true" || v == "1" || v == "yes"
		case "tags":
			for _, tag := range strings.Split(v, ",") {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					rl.Tags = append(rl.Tags, tag)
				}
			}
		}
	}

	if !found {
		return nil
	}
	return rl
}

// DisplayName returns the best display name for a container.
func DisplayName(riot *models.RiotLabels, containerName string) string {
	if riot != nil && riot.Name != "" {
		return riot.Name
	}
	return containerName
}

// GroupName returns the group name or "Ungrouped" as default.
func GroupName(riot *models.RiotLabels) string {
	if riot != nil && riot.Group != "" {
		return riot.Group
	}
	return "Ungrouped"
}

// InferRepoURL tries to find a GitHub/source repository URL from container labels and image name.
func InferRepoURL(labels map[string]string, image string) string {
	// 1. Check OCI standard labels
	for _, key := range []string{
		"org.opencontainers.image.source",
		"org.opencontainers.image.url",
		"org.label-schema.vcs-url",
	} {
		if v, ok := labels[key]; ok && v != "" {
			return v
		}
	}

	// 2. Heuristic: derive URL from image name
	img := image
	if at := strings.Index(img, "@"); at != -1 {
		img = img[:at]
	}
	if colon := strings.LastIndex(img, ":"); colon != -1 {
		img = img[:colon]
	}
	img = strings.TrimPrefix(img, "sha256:")

	// Skip raw sha256 digests
	if len(img) == 64 && !strings.Contains(img, "/") {
		return ""
	}

	// ghcr.io/owner/repo → github.com/owner/repo
	if strings.HasPrefix(img, "ghcr.io/") {
		parts := strings.SplitN(strings.TrimPrefix(img, "ghcr.io/"), "/", 3)
		if len(parts) >= 2 {
			return "https://github.com/" + parts[0] + "/" + parts[1]
		}
	}

	// lscr.io/linuxserver/name or linuxserver/name → github.com/linuxserver/docker-name
	cleaned := strings.TrimPrefix(img, "lscr.io/")
	if strings.HasPrefix(cleaned, "linuxserver/") {
		name := strings.TrimPrefix(cleaned, "linuxserver/")
		return "https://github.com/linuxserver/docker-" + name
	}

	// Docker Hub library images
	if !strings.Contains(img, "/") && !strings.Contains(img, ".") {
		return "https://hub.docker.com/_/" + img
	}

	// Docker Hub user/repo
	if !strings.Contains(img, ".") {
		parts := strings.SplitN(img, "/", 2)
		if len(parts) == 2 {
			return "https://hub.docker.com/r/" + parts[0] + "/" + parts[1]
		}
	}

	return ""
}
