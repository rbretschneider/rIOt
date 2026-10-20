package redact

import (
	"encoding/json"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"testing"
)

func TestDUX_SEC003_SharedFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases struct {
		Mask []struct{ Input, Want string }
		Keys []struct {
			Input string
			Want  bool
		}
		Env []struct{ Key, Value, Want string }
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases.Mask {
		got, _ := MaskLine(c.Input)
		if got != c.Want {
			t.Errorf("mask %q: %q != %q", c.Input, got, c.Want)
		}
	}
	for _, c := range cases.Keys {
		if SensitiveKey(c.Input) != c.Want {
			t.Errorf("key %q", c.Input)
		}
	}
	for _, c := range cases.Env {
		got, _ := EnvPair(c.Key, c.Value)
		if got != c.Want {
			t.Errorf("env %q", c.Key)
		}
	}
}
func TestDUX_AC062_InspectRedaction(t *testing.T) {
	input := `{"Config":{"Env":["TZ=UTC","DATABASE_URL=hunter2"],"Cmd":["login","--password","hunter2"],"Healthcheck":{"Test":["CMD","curl","--clientKey=hunter2"]}},"HostConfig":{"LogConfig":{"Config":{"syslog-address":"tcp://:hunter2@h"}}}}`
	out, n, err := InspectJSON([]byte(input))
	if err != nil || n < 4 || strings.Contains(string(out), "hunter2") {
		t.Fatalf("redaction failed: %s %d %v", out, n, err)
	}
}
func TestDUX_AC083_ComposeEnvAllValuesHidden(t *testing.T) {
	input := "services:\n  web:\n    environment:\n      DATABASE_URL: hunter2\n      APP_MODE: hunter2\n      TZ: UTC\n    command: curl --password hunter2\n"
	out, n, err := ComposeFile([]byte(input))
	if err != nil || n < 3 || strings.Contains(string(out), "hunter2") {
		t.Fatalf("redaction failed: %s %d %v", out, n, err)
	}
}
func TestDUX_SEC003_DotEnvFailsClosed(t *testing.T) {
	for _, input := range []string{"KEY='first\nsecond", "KEY=first\\\nsecond", "not a key=hunter2"} {
		out, _, err := DotEnv([]byte(input))
		if err == nil || out != nil {
			t.Fatalf("partial dotenv read: %+v", out)
		}
	}
}

func TestDUX_ComposePreservesLinesCommentsAliasesAndBlocks(t *testing.T) {
	input := "# retain header\nx-env: &env\n  APP_MODE: secret-app\n  TZ: UTC # retain TZ comment\nx-token: &token secret-anchor\nservices:\n  web:\n    environment:\n      <<: *env\n      PASSWORD: *token\n      MULTI: |\n        secret-first\n        secret-second\n      BLANK:\n    command: [curl, --user, user:secret-user]\n# URL postgres://u:secret-comment@example.internal/db\n"
	out, n, err := ComposeFile([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if n < 5 {
		t.Fatal("missing scalar redactions", n)
	}
	if strings.Count(string(out), "\n") != strings.Count(input, "\n") {
		t.Fatal("line numbers changed", string(out))
	}
	for _, secret := range []string{"secret-app", "secret-anchor", "secret-first", "secret-second", "secret-user", "secret-comment"} {
		if strings.Contains(string(out), secret) {
			t.Fatalf("secret %q remained: %s", secret, out)
		}
	}
	for _, keep := range []string{"# retain header", "TZ: UTC # retain TZ comment", "<<: *env", "PASSWORD: *token"} {
		if !strings.Contains(string(out), keep) {
			t.Fatalf("unrelated source was changed: %q\n%s", keep, out)
		}
	}
	var parsed yaml.Node
	if err := yaml.Unmarshal(out, &parsed); err != nil {
		t.Fatal("masked compose is invalid YAML", err, string(out))
	}
}
func TestDUX_ComposeFlowAliasUsesSourceLexicalContext(t *testing.T) {
	input := "x: &secret secret,entire-value\nservices:\n  web:\n    environment: {PASSWORD: *secret}\n"
	out, _, err := ComposeFile([]byte(input))
	if err != nil || strings.Contains(string(out), "entire-value") {
		t.Fatalf("flow alias scalar suffix leaked: %s %v", out, err)
	}
}
func TestDUX_ComposePreservesQuotedMultilineAndUnicodeColumns(t *testing.T) {
	input := "services:\n  web:\n    labels: {name: café, password: 'secret-value'} # keep\n    environment:\n      PASSWORD: \"secret-one\n        secret-two\" # still here\n"
	out, _, err := ComposeFile([]byte(input))
	if err != nil || strings.Contains(string(out), "secret-") {
		t.Fatalf("quoted scalar leaked: %s %v", out, err)
	}
	if strings.Count(string(out), "\n") != strings.Count(input, "\n") || !strings.Contains(string(out), "# still here") || !strings.Contains(string(out), "café") {
		t.Fatalf("source layout changed: %s", out)
	}
	var parsed yaml.Node
	if yaml.Unmarshal(out, &parsed) != nil {
		t.Fatalf("invalid output: %s", out)
	}
}
func TestDUX_DotEnvQuotedMultilineAndASCIIKeys(t *testing.T) {
	input := "export APP.KEY-NAME='secret\ncontinued' # comment\nTZ=UTC # comment\nPASSWORD=unquoted-secret\nPATH=\"/safe\\n/bin\"\n"
	keys, n, err := DotEnv([]byte(input))
	if err != nil || n != 2 || len(keys) != 4 {
		t.Fatalf("dotenv failed: %+v %d %v", keys, n, err)
	}
	if keys[0].Value != "<redacted>" || keys[1].Value != "UTC" || keys[3].Value != "/safe\n/bin" {
		t.Fatalf("dotenv values wrong: %+v", keys)
	}
	if _, _, err := DotEnv([]byte("ÉNV=secret")); err == nil {
		t.Fatal("non-ASCII key accepted")
	}
}
func TestDUX_ArgvQuotedAndAttachedCredentials(t *testing.T) {
	for _, args := range [][]string{{"curl", "-uuser:secret-value"}, {"curl", "--user='user:secret-value'"}, {"sh", "-c", `curl --password "secret value"`}, {"--password", "secret with --token elsewhere", "safe"}} {
		out, n := Argv(args)
		joined := strings.Join(out, " ")
		if n == 0 || strings.Contains(joined, "secret-value") || strings.Contains(joined, "secret value") || strings.Contains(joined, "secret with") {
			t.Fatalf("argv credential leaked: %v -> %v", args, out)
		}
	}
}

func TestDUX_ComposeScalarStylesKeepExactPhysicalLines(t *testing.T) {
	cases := []string{
		"services:\n  web:\n    environment:\n      PASSWORD:",
		"services: {web: {environment: {PASSWORD: }}}\n",
		"services:\n  web:\n    environment:\n      PASSWORD: !!str &pw secret-sentinel\n",
		"services:\n  web:\n    environment:\n      PASSWORD: >- # retain indicator comment\n        secret-sentinel\n        continued-secret\n      TZ: UTC\n",
		"services:\r\n  web:\r\n    environment:\r\n      PASSWORD: \"secret-sentinel\r\n        continued-secret\" # comment\r\n",
		"services:\n  web:\n    environment:\n      PASSWORD: secret-sentinel\n        continued-secret\n      TZ: UTC\n",
	}
	for _, input := range cases {
		out, _, err := ComposeFile([]byte(input))
		if err != nil {
			t.Errorf("supported scalar refused: %q: %v", input, err)
			continue
		}
		if strings.Contains(string(out), "secret-sentinel") || strings.Contains(string(out), "continued-secret") {
			t.Errorf("scalar value leaked: %s", out)
		}
		if strings.Count(input, "\n") != strings.Count(string(out), "\n") {
			t.Errorf("physical lines changed: %s", out)
		}
		var node yaml.Node
		if err := yaml.Unmarshal(out, &node); err != nil {
			t.Errorf("masked scalar broke YAML: %s %v", out, err)
		}
	}
}
func TestDUX_DotEnvQuotedEscapesAndMalformedTail(t *testing.T) {
	keys, _, err := DotEnv([]byte("TOKEN=\"first\\\"second\nthird\" # comment\nTZ='UTC'\nPATH=\"/usr/bin\\t/bin\"\n"))
	if err != nil || len(keys) != 3 || keys[0].Value != "<redacted>" || keys[2].Value != "/usr/bin\t/bin" {
		t.Fatalf("quoted escapes: %+v %v", keys, err)
	}
	for _, input := range []string{"TOKEN='secret'garbage", "TOKEN=\"unterminated", "1TOKEN=secret", "export TOKEN\nPASSWORD=secret"} {
		if out, _, err := DotEnv([]byte(input)); err == nil || out != nil {
			t.Fatalf("malformed dotenv returned partial data: %q %+v", input, out)
		}
	}
}
