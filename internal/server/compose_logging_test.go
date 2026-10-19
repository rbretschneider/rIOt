package server

import (
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"testing"
)

func TestDUX_SEC038_ComposeLoggingOptions(t *testing.T) {
	for _, file := range []string{"../../docker-compose.yml", "../../docker-compose.prod.yml"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Services map[string]struct {
				Logging struct {
					Driver  string
					Options map[string]string
				}
				Volumes []string
			}
		}
		if err = yaml.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for name, s := range doc.Services {
			if s.Logging.Driver != "json-file" || s.Logging.Options["max-size"] != "10m" || s.Logging.Options["max-file"] != "5" {
				t.Errorf("unbounded Docker logs: %s %s", file, name)
			}
			for _, mount := range s.Volumes {
				if strings.HasPrefix(mount, "riot-data:") && name != "riot-db" {
					t.Errorf("database files exposed to %s", name)
				}
			}
		}
		found := false
		for _, mount := range doc.Services["riot-server"].Volumes {
			if mount == "riot-fsprobe:/riot-fsprobe:ro" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s missing readonly filesystem probe", file)
		}
	}
}
