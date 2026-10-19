package redact

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type EnvKey struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Argv masks credential switches without invoking a shell or interpreting substitutions.
func Argv(args []string) ([]string, int) { return redactArgv(args, 0) }

func redactEnv(s string) (string, int) {
	k, v, ok := strings.Cut(s, "=")
	if !ok {
		return s, 0
	}
	v, changed := EnvPair(k, v)
	if changed {
		return k + "=" + v, 1
	}
	return k + "=" + v, 0
}

func redactValue(v interface{}, key string, count *int) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			if SensitiveKey(k) && !strings.EqualFold(k, "environment") {
				out[k] = "<redacted>"
				*count++
				continue
			}
			if strings.EqualFold(k, "environment") {
				switch env := val.(type) {
				case map[string]interface{}:
					m := make(map[string]interface{}, len(env))
					for ek, ev := range env {
						if SafeEnv[ek] {
							m[ek] = redactValue(ev, ek, count)
						} else {
							m[ek] = "<redacted>"
							*count++
						}
					}
					out[k] = m
				default:
					out[k] = redactValue(val, "env", count)
				}
				continue
			}
			out[k] = redactValue(val, k, count)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		if strings.EqualFold(key, "cmd") || strings.EqualFold(key, "command") || strings.EqualFold(key, "entrypoint") || strings.EqualFold(key, "args") || strings.EqualFold(key, "test") {
			args := make([]string, len(x))
			allStrings := true
			for i, val := range x {
				s, ok := val.(string)
				if !ok {
					allStrings = false
					break
				}
				args[i] = s
			}
			if allStrings {
				args, n := Argv(args)
				*count += n
				for i, s := range args {
					out[i] = s
				}
				return out
			}
		}
		for i, val := range x {
			out[i] = redactValue(val, key, count)
		}
		return out
	case string:
		if strings.EqualFold(key, "env") {
			s, n := redactEnv(x)
			*count += n
			return s
		}
		if strings.EqualFold(key, "command") || strings.EqualFold(key, "entrypoint") || strings.EqualFold(key, "test") {
			// A shell string may contain quoting, newlines or expansions. Hiding it
			// in full avoids reconstructing a command with different semantics.
			*count++
			return "<redacted>"
		}
		s, n := MaskText(x, "<redacted>")
		*count += n
		return s
	default:
		return v
	}
}

func InspectJSON(data []byte) ([]byte, int, error) {
	if len(data) > 1<<20 {
		return nil, 0, errors.New("redaction_failed")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value interface{}
	if err := dec.Decode(&value); err != nil {
		return nil, 0, errors.New("redaction_failed")
	}
	if dec.Decode(new(interface{})) != io.EOF {
		return nil, 0, errors.New("redaction_failed")
	}
	count := 0
	value = redactValue(value, "", &count)
	out, err := json.MarshalIndent(value, "", "  ")
	return out, count, err
}

// ComposeFile masks scalar spans in the original bytes, preserving line numbers,
// comments, aliases, anchors, and formatting. Unsupported syntax fails closed.
func ComposeFile(data []byte) ([]byte, int, error) { return redactComposeSource(data) }

// DotEnv parses compose dotenv quoting without expansion or partial results.
func DotEnv(data []byte) ([]EnvKey, int, error) { return parseDotEnv(data) }
