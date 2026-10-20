package redact

import (
	"strings"
	"testing"
)

func TestDUX_SEC028_StoredLinesMaskedFromP0(t *testing.T) {
	for _, input := range []string{
		`password=hunter2`, `clientKey: hunter2`, `"DATABASE_PW":"hunter2"`,
		`redis://:hunter2@host`, `https://user:hunter2/a@b@host/path`,
		`https://host/path?apiKey=hunter2&safe=yes`,
		`Authorization: Bearer hunter2`, "-----BEGIN PRIVATE KEY-----\nhunter2\n-----END PRIVATE KEY-----",
	} {
		t.Run(input, func(t *testing.T) {
			masked, n := MaskLine(input)
			if strings.Contains(masked, "hunter2") || n == 0 {
				t.Fatalf("secret survived: %s", masked)
			}
			again, _ := MaskLine(masked)
			if again != masked {
				t.Fatalf("not idempotent: %q -> %q", masked, again)
			}
		})
	}
}

func TestDUX_SEC003_SensitiveKeys(t *testing.T) {
	for _, k := range []string{"clientKey", "PRIVKey", "DATABASE_PW", "MONKEY", "SAS", "signingKey", "connectionString", "traefik.auth.users"} {
		if !SensitiveKey(k) {
			t.Errorf("missed %s", k)
		}
	}
	if SensitiveKey("com.docker.compose.project") {
		t.Fatal("compose project marked sensitive")
	}
}
