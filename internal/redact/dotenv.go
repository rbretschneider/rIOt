package redact

import (
	"errors"
	"regexp"
	"strings"
)

var dotenvKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

func parseDotEnv(data []byte) ([]EnvKey, int, error) {
	fail := func() ([]EnvKey, int, error) { return nil, 0, errors.New("redaction_failed") }
	if len(data) > 256<<10 {
		return fail()
	}
	s := string(data)
	out := []EnvKey{}
	count := 0
	i := 0
	skipSpace := func() {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
			i++
		}
	}
	skipLine := func() {
		for i < len(s) && s[i] != '\n' {
			i++
		}
		if i < len(s) {
			i++
		}
	}
	for i < len(s) {
		skipSpace()
		if i >= len(s) {
			break
		}
		if s[i] == '\n' {
			i++
			continue
		}
		if s[i] == '#' {
			skipLine()
			continue
		}
		if strings.HasPrefix(s[i:], "export") && i+6 < len(s) && (s[i+6] == ' ' || s[i+6] == '\t') {
			i += 6
			skipSpace()
		}
		start := i
		for i < len(s) && s[i] != '=' && s[i] != '\n' {
			i++
		}
		if i >= len(s) || s[i] != '=' {
			return fail()
		}
		key := strings.TrimSpace(s[start:i])
		if !dotenvKey.MatchString(key) {
			return fail()
		}
		i++
		skipSpace()
		var value string
		if i < len(s) && (s[i] == '\'' || s[i] == '"') {
			quote := s[i]
			i++
			var b strings.Builder
			closed := false
			for i < len(s) {
				ch := s[i]
				i++
				if ch == quote {
					closed = true
					break
				}
				if ch == '\\' && i < len(s) {
					next := s[i]
					if quote == '\'' {
						if next == '\'' {
							b.WriteByte(next)
							i++
							continue
						}
						b.WriteByte(ch)
						continue
					}
					i++
					switch next {
					case 'n':
						b.WriteByte('\n')
					case 'r':
						b.WriteByte('\r')
					case 't':
						b.WriteByte('\t')
					case '\\', '"', '$':
						b.WriteByte(next)
					case '\n':
						b.WriteByte('\n')
					default:
						b.WriteByte('\\')
						b.WriteByte(next)
					}
					continue
				}
				if ch == '\r' && i < len(s) && s[i] == '\n' {
					continue
				}
				b.WriteByte(ch)
			}
			if !closed {
				return fail()
			}
			value = b.String()
			skipSpace()
			if i < len(s) && s[i] != '\n' && s[i] != '#' {
				return fail()
			}
			if i < len(s) && s[i] == '#' {
				skipLine()
			} else if i < len(s) {
				i++
			}
		} else {
			start = i
			for i < len(s) && s[i] != '\n' {
				i++
			}
			value = s[start:i]
			if i < len(s) {
				i++
			}
			for j := 0; j < len(value); j++ {
				if value[j] == '#' && (j == 0 || value[j-1] == ' ' || value[j-1] == '\t') {
					value = value[:j]
					break
				}
			}
			value = strings.TrimRight(value, " \t\r")
		}
		value, changed := EnvPair(key, value)
		if changed {
			count++
		}
		masked, n := MaskText(value, "<redacted>")
		count += n
		out = append(out, EnvKey{Key: key, Value: masked})
	}
	return out, count, nil
}
