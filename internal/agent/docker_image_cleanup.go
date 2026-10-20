package agent

import (
	"context"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"strings"
	"time"
)

// cleanupSupersededImages is deliberately opt-in. It never removes a tagged
// image, forces removal, or invokes any daemon-wide prune endpoint.
func (cr *cmdRun) cleanupSupersededImages(ids []string) {
	if cr.a.config == nil || !cr.a.config.Docker.PruneSupersededImages || len(ids) == 0 {
		return
	}
	cr.Step("cleanup_image")
	ctx, cancel := context.WithTimeout(cr.ctx, 30*time.Second)
	defer cancel()
	cli, err := cr.a.newRemovalDocker()
	if err != nil {
		cr.cleanupOutput("Image cleanup unavailable")
		return
	}
	defer cli.Close()
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] || !strings.HasPrefix(id, "sha256:") {
			continue
		}
		seen[id] = true
		inspect, err := cli.ImageInspect(ctx, id)
		if err != nil {
			cr.cleanupOutput("Skipped superseded image: no longer present")
			continue
		}
		if len(inspect.RepoTags) > 0 {
			cr.cleanupOutput("Skipped superseded image: still tagged")
			continue
		}
		containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
		if err != nil {
			cr.cleanupOutput("Skipped superseded image: usage could not be checked")
			continue
		}
		used := false
		for _, ctr := range containers {
			if ctr.ImageID == id {
				used = true
				break
			}
		}
		if used {
			cr.cleanupOutput("Skipped superseded image: still in use")
			continue
		}
		if _, err := cli.ImageRemove(ctx, id, image.RemoveOptions{Force: false, PruneChildren: false}); err != nil {
			cr.cleanupOutput("Superseded image removal refused")
		} else {
			cr.cleanupOutput("Removed unused superseded image " + id)
		}
	}
}
func (cr *cmdRun) cleanupOutput(output string) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	if len(cr.steps) == 0 {
		return
	}
	last := &cr.steps[len(cr.steps)-1]
	last.Output = models.TruncateTail(last.Output+output+"\n", 16<<10)
}
