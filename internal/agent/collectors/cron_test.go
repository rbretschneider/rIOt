package collectors

import (
	"strings"
	"testing"
)

func TestCronCollectorName(t *testing.T) {
	c := &CronCollector{}
	if c.Name() != "cron" {
		t.Errorf("expected Name() = %q, got %q", "cron", c.Name())
	}
}

func TestParseCrontabReader_UserCrontab(t *testing.T) {
	input := `# Edit this file to introduce tasks
# m h  dom mon dow   command
*/5 * * * * /usr/bin/backup.sh
0 3 * * 1 /opt/scripts/weekly-clean.sh --force
`

	jobs := ParseCrontabReader(strings.NewReader(input), "/var/spool/cron/crontabs/alice", "alice", false)

	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}

	// First job
	if jobs[0].User != "alice" {
		t.Errorf("expected user %q, got %q", "alice", jobs[0].User)
	}
	if jobs[0].Schedule != "*/5 * * * *" {
		t.Errorf("expected schedule %q, got %q", "*/5 * * * *", jobs[0].Schedule)
	}
	if jobs[0].Command != "/usr/bin/backup.sh" {
		t.Errorf("expected command %q, got %q", "/usr/bin/backup.sh", jobs[0].Command)
	}
	if jobs[0].Source != "/var/spool/cron/crontabs/alice" {
		t.Errorf("expected source %q, got %q", "/var/spool/cron/crontabs/alice", jobs[0].Source)
	}
	if !jobs[0].Enabled {
		t.Error("expected job to be enabled")
	}

	// Second job
	if jobs[1].Schedule != "0 3 * * 1" {
		t.Errorf("expected schedule %q, got %q", "0 3 * * 1", jobs[1].Schedule)
	}
	if jobs[1].Command != "/opt/scripts/weekly-clean.sh --force" {
		t.Errorf("expected command %q, got %q", "/opt/scripts/weekly-clean.sh --force", jobs[1].Command)
	}
}

func TestParseCrontabReader_SystemCrontab(t *testing.T) {
	input := `# /etc/crontab: system-wide crontab
SHELL=/bin/sh
PATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin

# m h dom mon dow user  command
17 *	* * *	root    cd / && run-parts --report /etc/cron.hourly
25 6	* * *	root	test -x /usr/sbin/anacron || ( cd / && run-parts --report /etc/cron.daily )
`

	jobs := ParseCrontabReader(strings.NewReader(input), "/etc/crontab", "", true)

	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}

	if jobs[0].User != "root" {
		t.Errorf("expected user %q, got %q", "root", jobs[0].User)
	}
	if jobs[0].Schedule != "17 * * * *" {
		t.Errorf("expected schedule %q, got %q", "17 * * * *", jobs[0].Schedule)
	}
	if jobs[0].Command != "cd / && run-parts --report /etc/cron.hourly" {
		t.Errorf("expected command %q, got %q", "cd / && run-parts --report /etc/cron.hourly", jobs[0].Command)
	}

	if jobs[1].User != "root" {
		t.Errorf("expected user %q, got %q", "root", jobs[1].User)
	}
}

func TestParseCrontabReader_SkipsCommentsBlankLinesEnvVars(t *testing.T) {
	input := `# This is a comment

MAILTO=admin@example.com
SHELL=/bin/bash
PATH=/usr/local/bin:/usr/bin

# Another comment
*/10 * * * * root /usr/bin/check.sh
`

	jobs := ParseCrontabReader(strings.NewReader(input), "/etc/cron.d/test", "", true)

	if len(jobs) != 1 {
		t.Fatalf("expected 1 job (comments, blank lines, and env vars skipped), got %d", len(jobs))
	}

	if jobs[0].Command != "/usr/bin/check.sh" {
		t.Errorf("expected command %q, got %q", "/usr/bin/check.sh", jobs[0].Command)
	}
	if jobs[0].User != "root" {
		t.Errorf("expected user %q, got %q", "root", jobs[0].User)
	}
}

func TestParseCrontabReader_SystemCrontabCronD(t *testing.T) {
	// /etc/cron.d files use system format (with user field)
	input := `# Certbot renewal
0 */12 * * * root test -x /usr/bin/certbot -a \! -d /run/systemd/system && perl -e 'sleep int(rand(43200))' && certbot -q renew
`

	jobs := ParseCrontabReader(strings.NewReader(input), "/etc/cron.d/certbot", "", true)

	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	if jobs[0].User != "root" {
		t.Errorf("expected user %q, got %q", "root", jobs[0].User)
	}
	if jobs[0].Schedule != "0 */12 * * *" {
		t.Errorf("expected schedule %q, got %q", "0 */12 * * *", jobs[0].Schedule)
	}
	if jobs[0].Source != "/etc/cron.d/certbot" {
		t.Errorf("expected source %q, got %q", "/etc/cron.d/certbot", jobs[0].Source)
	}
}

func TestIsEnvAssignment(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"MAILTO=admin@example.com", true},
		{"SHELL=/bin/bash", true},
		{"PATH=/usr/local/bin:/usr/bin", true},
		{"*/5 * * * * /usr/bin/test.sh", false},
		{"0 3 * * 1 root /usr/bin/test.sh", false},
		{"FOO_BAR=baz", true},
		{"", false},
	}

	for _, tt := range tests {
		got := isEnvAssignment(tt.line)
		if got != tt.want {
			t.Errorf("isEnvAssignment(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestParseCrontabReader_EmptyInput(t *testing.T) {
	jobs := ParseCrontabReader(strings.NewReader(""), "test", "user", false)
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs for empty input, got %d", len(jobs))
	}
}

func TestParseCrontabReader_ShortLineSkipped(t *testing.T) {
	// Lines with fewer than 6 fields (user crontab) should be skipped
	input := `* * * *
*/5 * * * * /usr/bin/valid.sh
`

	jobs := ParseCrontabReader(strings.NewReader(input), "test", "user", false)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job (short line skipped), got %d", len(jobs))
	}
	if jobs[0].Command != "/usr/bin/valid.sh" {
		t.Errorf("expected command %q, got %q", "/usr/bin/valid.sh", jobs[0].Command)
	}
}

func TestUserCrontabDirs_ContainsBothDistroVariants(t *testing.T) {
	// Verify that userCrontabDirs includes paths for both Debian and Red Hat variants
	debianFound := false
	redhatFound := false
	for _, dir := range userCrontabDirs {
		if dir == "/var/spool/cron/crontabs" {
			debianFound = true
		}
		if dir == "/var/spool/cron" {
			redhatFound = true
		}
	}
	if !debianFound {
		t.Error("userCrontabDirs missing Debian path /var/spool/cron/crontabs")
	}
	if !redhatFound {
		t.Error("userCrontabDirs missing Red Hat path /var/spool/cron")
	}
}

func TestCollectLinuxCrontabs_UsesFirstExistingDir(t *testing.T) {
	// The collector should use the first directory that exists and not double-count
	// when both paths resolve to the same content. We verify this by checking
	// that the Debian path is tried first (index 0) since it's more specific.
	if userCrontabDirs[0] != "/var/spool/cron/crontabs" {
		t.Errorf("expected Debian path first (more specific), got %q", userCrontabDirs[0])
	}
	if userCrontabDirs[1] != "/var/spool/cron" {
		t.Errorf("expected Red Hat path second, got %q", userCrontabDirs[1])
	}
}
