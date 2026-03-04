package collectors

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

type DockerCollector struct{}

func (c *DockerCollector) Name() string { return "docker" }

func (c *DockerCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.DockerInfo{}

	// Check if Docker is installed
	dockerPath, err := exec.LookPath("docker")
	if err != nil || dockerPath == "" {
		return info, nil
	}

	// Docker version
	out, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		return info, nil // Docker installed but daemon not running
	}
	info.Version = strings.TrimSpace(string(out))

	// Container list with stats
	out, err = exec.CommandContext(ctx, "docker", "ps", "-a",
		"--format", `{"name":"{{.Names}}","image":"{{.Image}}","status":"{{.Status}}","id":"{{.ID}}","ports":"{{.Ports}}"}`).Output()
	if err != nil {
		return info, nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		var container models.ContainerInfo
		if err := json.Unmarshal([]byte(line), &container); err != nil {
			continue
		}
		info.Containers = append(info.Containers, container)
		info.TotalContainers++
		if strings.HasPrefix(container.Status, "Up") {
			info.Running++
		} else {
			info.Stopped++
		}
	}

	return info, nil
}
