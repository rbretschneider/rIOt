package agent

import (
	"encoding/json"
	"fmt"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/errdefs"
	"sort"
	"strings"
	"time"
)

type removeItem struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Tags      []string        `json:"tags"`
	CreatedAt json.RawMessage `json:"created_at"`
}

func (a *Agent) dockerRemoveItems(cr *cmdRun) commandExecResult {
	kind, _ := cr.payload.Params["item_kind"].(string)
	if kind == "" {
		kind, _ = cr.payload.Params["kind"].(string)
	}
	raw, _ := json.Marshal(cr.payload.Params["items"])
	var items []removeItem
	if json.Unmarshal(raw, &items) != nil || len(items) == 0 || len(items) > 500 {
		return precheckFailure("select between 1 and 500 items")
	}
	if kind != "image" && kind != "volume" && kind != "network" {
		return precheckFailure("invalid inventory kind")
	}
	if key, h, held := a.locks.HeldWithPrefix(models.TargetKeyDockerPrefix); held && h.CmdID != cr.id {
		return conflictResult(key, h)
	}
	if key, h, ok := cr.lock("docker:host"); !ok {
		return conflictResult(key, h)
	}
	cli, err := a.newRemovalDocker()
	if err != nil {
		return precheckFailure("docker unavailable")
	}
	defer cli.Close()
	results := []map[string]string{}
	failures := 0
	cr.Step("remove")
	for _, item := range items {
		result := map[string]string{"id": item.ID, "name": item.Name, "status": "skipped"}
		err = func() error {
			cs, e := cli.ContainerList(cr.ctx, container.ListOptions{All: true})
			if e != nil {
				return e
			}
			switch kind {
			case "image":
				if !strings.HasPrefix(item.ID, "sha256:") || len(item.ID) != 71 {
					return fmt.Errorf("invalid image identity")
				}
				insp, e := cli.ImageInspect(cr.ctx, item.ID)
				if e != nil {
					return e
				}
				expected := append([]string(nil), item.Tags...)
				actual := append([]string(nil), insp.RepoTags...)
				sort.Strings(expected)
				sort.Strings(actual)
				if strings.Join(expected, "\x00") != strings.Join(actual, "\x00") {
					return fmt.Errorf("image tags changed")
				}
				for _, c := range cs {
					if c.ImageID == item.ID {
						return fmt.Errorf("image is in use")
					}
				}
				for _, tag := range expected {
					current, e := cli.ImageInspect(cr.ctx, tag)
					if e != nil || current.ID != item.ID {
						return fmt.Errorf("image tag changed")
					}
					if _, e = cli.ImageRemove(cr.ctx, tag, image.RemoveOptions{Force: false, PruneChildren: false}); e != nil {
						return e
					}
				}
				_, e = cli.ImageRemove(cr.ctx, item.ID, image.RemoveOptions{Force: false, PruneChildren: false})
				if errdefs.IsNotFound(e) {
					return nil
				}
				return e
			case "volume":
				if item.Name == "" || strings.ContainsAny(item.Name, "/\\\x00") {
					return fmt.Errorf("invalid volume identity")
				}
				v, e := cli.VolumeInspect(cr.ctx, item.Name)
				if e != nil {
					return e
				}
				var expected string
				json.Unmarshal(item.CreatedAt, &expected)
				if expected == "" || v.CreatedAt != expected {
					return fmt.Errorf("volume identity changed")
				}
				for _, c := range cs {
					for _, m := range c.Mounts {
						if m.Name == item.Name {
							return fmt.Errorf("volume is in use")
						}
					}
				}
				return cli.VolumeRemove(cr.ctx, item.Name, false)
			case "network":
				if len(item.ID) != 64 {
					return fmt.Errorf("invalid network identity")
				}
				n, e := cli.NetworkInspect(cr.ctx, item.ID, network.InspectOptions{})
				if e != nil {
					return e
				}
				if n.Name == "bridge" || n.Name == "host" || n.Name == "none" || n.Ingress {
					return fmt.Errorf("built-in network")
				}
				var expected time.Time
				if json.Unmarshal(item.CreatedAt, &expected) != nil || !n.Created.Equal(expected) {
					return fmt.Errorf("network identity changed")
				}
				for _, c := range cs {
					if c.NetworkSettings != nil {
						for _, net := range c.NetworkSettings.Networks {
							if net.NetworkID == item.ID {
								return fmt.Errorf("network is in use")
							}
						}
					}
				}
				return cli.NetworkRemove(cr.ctx, item.ID)
			}
			return nil
		}()
		if err != nil {
			failures++
			result["reason"] = "identity changed, in use, or removal refused"
		} else {
			result["status"] = "removed"
		}
		results = append(results, result)
	}
	a.triggerTelemetry()
	output, _ := json.Marshal(results)
	status := "success"
	if failures == len(items) {
		status = "error"
	}
	return commandExecResult{Status: status, Step: "remove", Message: fmt.Sprintf("removed %d; skipped %d", len(items)-failures, failures), Output: string(output)}
}
