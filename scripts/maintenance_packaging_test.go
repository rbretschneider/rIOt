package scripts

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readPackagingFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func TestMaintenanceServiceIsolation(t *testing.T) {
	t.Run("[MAINT-RECOVERY AC-002] fixed service owns transaction independently of agent", func(t *testing.T) {
		unit := readPackagingFile(t, "riot-maintenance.service")
		properties := map[string]string{}
		for _, line := range strings.Split(unit, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				t.Fatalf("invalid unit line: %q", line)
			}
			if _, exists := properties[key]; exists {
				t.Fatalf("duplicate unit directive: %s", key)
			}
			properties[key] = value
		}
		for key, expected := range map[string]string{
			"Type": "exec", "ExecStart": "/usr/local/lib/riot/riot-maintenance",
			"User": "root", "WorkingDirectory": "/", "Restart": "no",
			"StandardInput": "null", "StandardOutput": "journal", "StandardError": "journal",
			"RuntimeMaxSec": "35min", "TimeoutStopSec": "90s", "KillSignal": "SIGTERM", "KillMode": "control-group",
		} {
			if properties[key] != expected {
				t.Errorf("%s = %q; want %q", key, properties[key], expected)
			}
		}
		for _, forbidden := range []string{"PartOf", "BindsTo", "Requires", "MemoryMax", "Environment", "WantedBy", "ExecStartPre", "ExecStop"} {
			if value, exists := properties[forbidden]; exists {
				t.Errorf("maintenance unit must not inherit agent lifetime, environment, limits or boot replay: %s=%s", forbidden, value)
			}
		}
	})
}

// Run the actual workflow's checksum script, rather than reproducing its glob
// ordering in Go: legacy agents select the first matching platform suffix.
func TestMaintenanceReleaseChecksums(t *testing.T) {
	t.Run("[MAINT-RECOVERY AC-011] legacy platform lookup selects signed agent before both helpers", func(t *testing.T) {
		bash := "bash"
		if runtime.GOOS == "windows" {
			// Windows' system bash.exe is a WSL launcher, not a local shell.
			bash = filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe")
			if _, err := os.Stat(bash); err != nil {
				t.Skip("checksum workflow requires Linux bash or Git Bash")
			}
		}
		workflow := readPackagingFile(t, filepath.Join("..", ".github", "workflows", "release.yml"))
		_, step, found := strings.Cut(workflow, "      - name: Generate checksums\n")
		if !found {
			t.Fatal("checksum release step not found")
		}
		_, step, found = strings.Cut(step, "        run: |\n")
		if !found {
			t.Fatal("checksum shell body not found")
		}
		var script strings.Builder
		for _, line := range strings.Split(step, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if !strings.HasPrefix(line, "          ") {
				break
			}
			script.WriteString(strings.TrimPrefix(line, "          ") + "\n")
		}
		if script.Len() == 0 {
			t.Fatal("empty checksum shell body")
		}
		dir := t.TempDir()
		platforms := []string{"linux-amd64", "linux-386", "linux-arm64", "linux-armv7", "linux-armv6", "darwin-amd64", "darwin-arm64", "windows-amd64.exe"}
		assets := []string{"install.sh", "riot-maintenance.service"}
		for _, suffix := range platforms {
			assets = append(assets, "riot-agent-"+suffix)
			if strings.HasPrefix(suffix, "linux-") {
				assets = append(assets, "riot-agent-apply-"+suffix, "riot-maintenance-"+suffix)
			}
		}
		for _, name := range assets {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command(bash, "-e", "-c", "export PATH=/usr/bin:/bin:$PATH\n"+script.String())
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("release checksum script: %v\n%s", err, output)
		}
		manifest := readPackagingFile(t, filepath.Join(dir, "checksums.txt"))
		entries := map[string]string{}
		lines := []string{}
		scanner := bufio.NewScanner(strings.NewReader(manifest))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) != 2 {
				t.Fatalf("malformed manifest entry %q", scanner.Text())
			}
			fields[1] = strings.TrimPrefix(fields[1], "*")
			if _, duplicate := entries[fields[1]]; duplicate {
				t.Fatalf("duplicate manifest asset %q", fields[1])
			}
			entries[fields[1]] = fields[0]
			lines = append(lines, fields[1])
		}
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
		if len(entries) != len(assets) {
			t.Fatalf("manifest includes %d assets; want %d", len(entries), len(assets))
		}
		for _, asset := range assets {
			digest := sha256.Sum256([]byte(asset))
			if entries[asset] != hex.EncodeToString(digest[:]) {
				t.Errorf("missing or incorrect checksum for %s", asset)
			}
		}
		for _, suffix := range platforms {
			for _, asset := range lines {
				if strings.Contains(asset, suffix) {
					if expected := "riot-agent-" + suffix; asset != expected {
						t.Errorf("legacy %s lookup selected %s instead of %s", suffix, asset, expected)
					}
					break
				}
			}
		}
	})
}
