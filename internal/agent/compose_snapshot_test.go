package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/api/types/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func snapshotOf(t *testing.T, d *fakeDocker) daemonSnapshot {
	t.Helper()
	snap, err := loadDaemonSnapshot(context.Background(), d)
	require.NoError(t, err)
	return snap
}

// A compose-built image carries only project, service and version labels.
var composeBuiltImageLabels = map[string]string{
	models.LabelComposeProject: "stack", models.LabelComposeService: "web", "com.docker.compose.version": "2.40.3",
}

func TestSnapshot_AC018_ImageDefinedWorkingDirExcluded(t *testing.T) {
	d := newFakeDocker()
	c := d.add(fakeContainer{Name: "hostile", ImageID: "sha256:hostile", Labels: composeLabels("/srv/media", "web")})
	d.imageLabels["sha256:hostile"] = map[string]string{models.LabelComposeWorkingDir: "/srv/media"}
	ok := d.add(fakeContainer{Name: "honest", ImageID: "sha256:honest", Labels: composeLabels("/srv/media", "db")})

	snap := snapshotOf(t, d)

	assert.Equal(t, excludedImageLabel, snap.excluded[c.ID], "[AC-018] a container whose image defines the working-dir label is not a compose source")
	assert.NotContains(t, snap.excluded, ok.ID, "[AC-018] an ordinary compose container is a source")
	var ids []string
	for _, s := range snap.composeSources() {
		ids = append(ids, s.ID)
	}
	assert.Equal(t, []string{ok.ID}, ids, "[AC-018] composeSources leaves out the excluded container")
}

func TestSnapshot_AC018_ComposeBuiltImageLabelsAccepted(t *testing.T) {
	d := newFakeDocker()
	c := d.add(fakeContainer{Name: "built", ImageID: "sha256:built", Labels: composeLabels("/srv/media", "web")})
	d.imageLabels["sha256:built"] = composeBuiltImageLabels

	snap := snapshotOf(t, d)

	assert.NotContains(t, snap.excluded, c.ID, "[AC-018] build: stacks carry only project/service/version labels on the image and are unaffected")
	assert.Len(t, snap.composeSources(), 1)
}

func TestSnapshot_AC018_ImageInspectFailureExcludes(t *testing.T) {
	captureLogs(t)
	d := newFakeDocker()
	c := d.add(fakeContainer{Name: "blind", ImageID: "sha256:gone", Labels: composeLabels("/srv/media", "web")})
	d.failIf = func(call string) error {
		if call == "imageinspect:sha256:gone" {
			return errors.New("No such image")
		}
		return nil
	}

	snap := snapshotOf(t, d)

	assert.Equal(t, excludedImageUnread, snap.excluded[c.ID], "[AC-018] an image that cannot be inspected fails closed")
}

// Condition 5: the rule triggers on the key being present, even with an empty value.
func TestSnapshot_AC018_EmptyValueImageLabelExcluded(t *testing.T) {
	d := newFakeDocker()
	c := d.add(fakeContainer{Name: "sneaky", ImageID: "sha256:sneaky", Labels: composeLabels("/srv/media", "web")})
	d.imageLabels["sha256:sneaky"] = map[string]string{models.LabelComposeWorkingDir: ""}

	snap := snapshotOf(t, d)

	assert.Equal(t, excludedImageLabel, snap.excluded[c.ID], "[AC-018] key presence, not value, triggers the provenance rule")
}

func TestSnapshot_AC018_ImageInspectedOncePerImage(t *testing.T) {
	d := newFakeDocker()
	d.add(fakeContainer{Name: "a", ImageID: "sha256:shared", Labels: composeLabels("/srv/media", "a")})
	d.add(fakeContainer{Name: "b", ImageID: "sha256:shared", Labels: composeLabels("/srv/media", "b")})
	d.add(fakeContainer{Name: "plain", ImageID: "sha256:other"}) // no working-dir label: never inspected

	snapshotOf(t, d)

	assert.Equal(t, []string{"imageinspect:sha256:shared"}, d.callsWithPrefix("imageinspect:"), "[AC-018] one inspect per distinct image of a labelled container")
}

func TestSnapshot_AC019_InfoOrListFailureIsDaemonUnavailable(t *testing.T) {
	for _, op := range []string{"info", "list"} {
		t.Run(op, func(t *testing.T) {
			d := newFakeDocker()
			d.failIf = func(call string) error {
				if call == op {
					return errors.New("daemon unreachable")
				}
				return nil
			}
			_, err := loadDaemonSnapshot(context.Background(), d)
			require.Error(t, err, "[AC-019] %s failure fails the snapshot", op)
		})
	}
}

func TestSnapshot_AC019_EmptyDataRootFailsClosed(t *testing.T) {
	d := newFakeDocker()
	_, err := loadDaemonSnapshot(context.Background(), emptyRootDocker{d})
	require.Error(t, err, "[AC-019] a daemon that does not report its data root cannot be checked")
}

type emptyRootDocker struct{ *fakeDocker }

func (emptyRootDocker) Info(context.Context) (system.Info, error) { return system.Info{}, nil }

func TestSnapshot_AC019_VolumeInspectFailureFailsClosed(t *testing.T) {
	d := newFakeDocker()
	d.add(fakeContainer{Name: "v", Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "data", Driver: "local", RW: true}}})
	d.failIf = func(call string) error {
		if call == "volumeinspect:data" {
			return errors.New("boom")
		}
		return nil
	}
	_, err := loadDaemonSnapshot(context.Background(), d)
	require.Error(t, err, "[AC-019] a volume that cannot be inspected could hide a bind device")
}

func TestSnapshot_AC019_BindDeviceVolumeRecorded(t *testing.T) {
	d := newFakeDocker()
	d.volumes["data"] = volume.Volume{Name: "data", Driver: "local", Options: map[string]string{"o": "bind", "device": "/srv/media", "type": "none"}}
	d.volumes["plain"] = volume.Volume{Name: "plain", Driver: "local"}
	d.add(fakeContainer{Name: "v", Mounts: []container.MountPoint{
		{Type: mount.TypeVolume, Name: "data", Driver: "local", RW: true},
		{Type: mount.TypeVolume, Name: "plain", Driver: "local", RW: true},
		{Type: mount.TypeVolume, Name: "remote", Driver: "nfs-plugin", RW: true},
	}})

	snap := snapshotOf(t, d)

	assert.Equal(t, map[string]string{"data": "/srv/media"}, snap.volumeDevices, "[AC-019] only local bind-device volumes are recorded")
	assert.Empty(t, d.callsWithPrefix("volumeinspect:remote"), "non-local volume drivers are not inspected")
}

func TestSnapshot_AC009_ProjectResolution(t *testing.T) {
	d := newFakeDocker()
	d.add(fakeContainer{Name: "a", ImageID: "sha256:a", Labels: composeLabels("/srv/media", "a")})
	d.add(fakeContainer{Name: "b", ImageID: "sha256:a", Labels: composeLabels("/srv/media/", "b")}) // same dir, trailing slash
	snap := snapshotOf(t, d)

	dir, err := snap.resolveProject("stack")
	require.Nil(t, err)
	assert.Equal(t, "/srv/media", dir, "[AC-009] the label value, verbatim")

	_, err = snap.resolveProject("nope")
	require.NotNil(t, err)
	assert.Equal(t, ruleProjectNotFound, err.Rule)

	_, err = snap.resolveProject("Bad Name")
	require.NotNil(t, err)
	assert.Equal(t, ruleInvalidProject, err.Rule)

	d.add(fakeContainer{Name: "c", ImageID: "sha256:a", Labels: composeLabels("/srv/other", "c")})
	_, err = snapshotOf(t, d).resolveProject("stack")
	require.NotNil(t, err)
	assert.Equal(t, ruleAmbiguousProject, err.Rule, "[AC-009] two working directories for one project")
}
