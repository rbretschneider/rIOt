package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// DockerCollector collects container and daemon info via the Docker SDK.
type DockerCollector struct {
	CollectStats bool
	SocketPath   string
}

func (c *DockerCollector) Name() string { return "docker" }

func (c *DockerCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.DockerInfo{}

	cli, err := c.newClient()
	if err != nil {
		// Docker not available — graceful degradation
		return info, nil
	}
	defer cli.Close()

	// Ping to verify daemon is running
	if _, err := cli.Ping(ctx); err != nil {
		return info, nil
	}
	info.Available = true

	// Daemon info
	sysInfo, err := cli.Info(ctx)
	if err == nil {
		fillDaemonInfo(info, sysInfo)
	}

	// Docker version
	ver, err := cli.ServerVersion(ctx)
	if err == nil {
		info.Version = ver.Version
		info.APIVersion = ver.APIVersion
	}

	// List all containers
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		slog.Warn("docker: failed to list containers", "error", err)
		return info, nil
	}

	info.TotalContainers = len(containers)
	for _, c := range containers {
		ci := containerFromAPI(c)
		switch ci.State {
		case "running":
			info.Running++
		case "paused":
			info.Paused++
		default:
			info.Stopped++
		}
		info.Containers = append(info.Containers, ci)
	}

	// Collect CPU/mem stats for running containers
	if c.CollectStats {
		c.collectStats(ctx, cli, info.Containers)
	}

	return info, nil
}

func (c *DockerCollector) newClient() (*client.Client, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if c.SocketPath != "" {
		host := "unix://" + c.SocketPath
		if runtime.GOOS == "windows" {
			host = "npipe://" + c.SocketPath
		}
		opts = append(opts, client.WithHost(host))
	}
	return client.NewClientWithOpts(opts...)
}

func fillDaemonInfo(info *models.DockerInfo, sys system.Info) {
	info.ImagesTotal = sys.Images
	info.StorageDriver = sys.Driver
	info.DockerRootDir = sys.DockerRootDir
}

func containerFromAPI(c container.Summary) models.ContainerInfo {
	name := ""
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}

	shortID := c.ID
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}

	// Deduplicate port mappings
	seen := make(map[string]struct{})
	var ports []models.PortMapping
	for _, p := range c.Ports {
		if p.PublicPort == 0 {
			continue
		}
		key := fmt.Sprintf("%s:%d->%d/%s", p.IP, p.PublicPort, p.PrivatePort, p.Type)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		ports = append(ports, models.PortMapping{
			ContainerPort: portStr(p.PrivatePort, p.Type),
			HostPort:      portStr(p.PublicPort, ""),
			Protocol:      p.Type,
			HostIP:        p.IP,
		})
	}

	// Parse mounts
	var mounts []models.MountInfo
	for _, m := range c.Mounts {
		mounts = append(mounts, models.MountInfo{
			Type:        string(m.Type),
			Source:      m.Source,
			Destination: m.Destination,
			ReadOnly:    !m.RW,
		})
	}

	// Parse networks
	var networks []models.NetworkAttach
	if c.NetworkSettings != nil {
		for netName, net := range c.NetworkSettings.Networks {
			networks = append(networks, models.NetworkAttach{
				Name:      netName,
				IPAddress: net.IPAddress,
				Gateway:   net.Gateway,
				MacAddr:   net.MacAddress,
			})
		}
	}

	riot := ParseRiotLabels(c.Labels)
	repoURL := InferRepoURL(c.Labels, c.Image)

	ci := models.ContainerInfo{
		ID:       c.ID,
		ShortID:  shortID,
		Name:     name,
		Image:    c.Image,
		State:    c.State,
		Status:   c.Status,
		Created:  c.Created,
		Ports:    ports,
		Labels:   c.Labels,
		Mounts:   mounts,
		Networks: networks,
		RepoURL:  repoURL,
		Riot:     riot,
	}

	return ci
}

// collectStats gathers CPU/mem for running containers.
func (c *DockerCollector) collectStats(ctx context.Context, cli *client.Client, containers []models.ContainerInfo) {
	for i := range containers {
		if containers[i].State != "running" {
			continue
		}
		resp, err := cli.ContainerStats(ctx, containers[i].ID, false)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || len(body) == 0 {
			continue
		}

		var stats struct {
			CPUStats struct {
				CPUUsage struct {
					TotalUsage uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
				SystemCPUUsage uint64 `json:"system_cpu_usage"`
				OnlineCPUs     uint32 `json:"online_cpus"`
			} `json:"cpu_stats"`
			PreCPUStats struct {
				CPUUsage struct {
					TotalUsage uint64 `json:"total_usage"`
				} `json:"cpu_usage"`
				SystemCPUUsage uint64 `json:"system_cpu_usage"`
			} `json:"precpu_stats"`
			MemoryStats struct {
				Usage uint64 `json:"usage"`
				Limit uint64 `json:"limit"`
				Stats struct {
					Cache uint64 `json:"cache"`
				} `json:"stats"`
			} `json:"memory_stats"`
		}
		if err := json.Unmarshal(body, &stats); err != nil {
			continue
		}

		// CPU percent calculation
		cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
		sysDelta := float64(stats.CPUStats.SystemCPUUsage - stats.PreCPUStats.SystemCPUUsage)
		if sysDelta > 0 && stats.CPUStats.OnlineCPUs > 0 {
			containers[i].CPUPercent = (cpuDelta / sysDelta) * float64(stats.CPUStats.OnlineCPUs) * 100.0
		}

		containers[i].MemUsage = int64(stats.MemoryStats.Usage - stats.MemoryStats.Stats.Cache)
		containers[i].MemLimit = int64(stats.MemoryStats.Limit)
	}
}

func portStr(port uint16, proto string) string {
	if port == 0 {
		return ""
	}
	if proto != "" {
		return fmt.Sprintf("%d/%s", port, proto)
	}
	return fmt.Sprintf("%d", port)
}
