//go:build linux

package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaintenance_AC012_NegativePrivilegesAreListedNeverExecuted(t *testing.T) {
	for allowed := 0; allowed < 3; allowed++ {
		calls := 0
		err := maintenanceDeniedPrivileges(context.Background(), func(_ context.Context, name string, args ...string) ([]byte, error) {
			require.Equal(t, "sudo", name)
			require.Equal(t, []string{"-n", "-l"}, args[:2])
			i := calls
			calls++
			if i == allowed {
				return nil, nil
			}
			return nil, errors.New("not permitted")
		})
		require.Error(t, err)
		require.Equal(t, allowed+1, calls)
	}
}

func TestMaintenance_AC012_EffectiveUnitPolicyRejectsUnsafeOverrides(t *testing.T) {
	policy := "FragmentPath=/etc/systemd/system/riot-maintenance.service\nUser=root\nExecStart={ path=/usr/local/lib/riot/riot-maintenance ; argv[]=/usr/local/lib/riot/riot-maintenance ; }\nPartOf=\nBindsTo=\nRequires=sysinit.target\nStandardInput=null\nStandardOutput=journal\nStandardError=journal\nRestart=no\nKillMode=control-group\nRuntimeMaxUSec=35min\nTimeoutStopUSec=1min 30s\nType=exec\nWorkingDirectory=/\n"
	require.NoError(t, maintenanceUnitPolicy(policy))
	for _, bad := range []struct{ from, to string }{{"Requires=sysinit.target", "Requires=riot-agent.service"}, {"RuntimeMaxUSec=35min", "RuntimeMaxUSec=infinity"}, {"KillMode=control-group", "KillMode=process"}, {"StandardInput=null", "StandardInput=socket"}, {"StandardError=journal", "StandardError=socket"}, {"Restart=no", "Restart=always"}, {"argv[]=/usr/local/lib/riot/riot-maintenance ;", "argv[]=/usr/local/lib/riot/riot-maintenance --user-path=/tmp ;"}} {
		require.Error(t, maintenanceUnitPolicy(strings.Replace(policy, bad.from, bad.to, 1)))
	}
	require.NoError(t, maintenanceAgentPolicy("User=riot\nExecStartPre={ path=/usr/local/lib/riot/riot-agent-apply ; argv[]=/usr/local/lib/riot/riot-agent-apply --boot ; }\n"))
	require.Error(t, maintenanceAgentPolicy("User=riot\nExecStartPre={ path=/bin/sh ; argv[]=/bin/sh -c cp /tmp/agent /usr/local/bin/riot-agent ; }\n"))
	require.Error(t, maintenanceAgentPolicy("User=root\nExecStartPre=\n"))
}

func TestMaintenance_AC012_EffectiveSudoPolicyDetectsLegacyRootShells(t *testing.T) {
	for _, policy := range []string{"    (root) NOPASSWD: /bin/sh -c *", "    (ALL : ALL) NOPASSWD: ALL", "    (root) NOPASSWD: /usr/bin/systemd-run *", "    (root) NOPASSWD: /usr/bin/apt-get -y dist-upgrade"} {
		require.Error(t, maintenanceSudoPolicy(policy))
	}
	require.NoError(t, maintenanceSudoPolicy("    (root) NOPASSWD: /usr/bin/systemctl start --no-block riot-maintenance.service\n    (root) NOPASSWD: /usr/bin/apt-mark hold *, /usr/bin/apt-mark unhold *"))
}
