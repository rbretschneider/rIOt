package redact

import "strings"

func redactArgv(args []string, depth int) ([]string, int) {
	out := append([]string(nil), args...)
	count := 0
	hideNext, userNext := false, false
	replace := func(i int, value string) {
		if out[i] != value {
			out[i] = value
			count++
		}
	}
	for i, arg := range args {
		if hideNext {
			replace(i, "<redacted>")
			hideNext = false
			continue
		}
		if userNext {
			value := strings.Trim(arg, "\"'")
			if user, _, found := strings.Cut(value, ":"); found {
				replace(i, user+":<redacted>")
			}
			userNext = false
			continue
		}
		if strings.ContainsAny(arg, " \t\r\n") {
			if depth >= 4 {
				replace(i, "<redacted>")
				continue
			}
			words, ok := credentialWords(arg)
			if !ok {
				replace(i, "<redacted>")
				continue
			}
			if len(words) > 1 || len(words) == 1 && words[0] != arg {
				if _, n := redactArgv(words, depth+1); n > 0 {
					replace(i, "<redacted>")
					continue
				}
			}
		}
		token := strings.Trim(arg, "\"'")
		if token == "-u" || token == "--user" {
			userNext = true
			continue
		}
		if strings.HasPrefix(token, "--user=") {
			value := strings.Trim(strings.TrimPrefix(token, "--user="), "\"'")
			if user, _, found := strings.Cut(value, ":"); found {
				replace(i, "--user="+user+":<redacted>")
			}
			continue
		}
		if strings.HasPrefix(token, "-u") && len(token) > 2 {
			value := strings.TrimPrefix(token[2:], "=")
			value = strings.Trim(value, "\"'")
			if user, _, found := strings.Cut(value, ":"); found {
				replace(i, "-u"+user+":<redacted>")
			}
			continue
		}
		if strings.HasPrefix(token, "--") {
			key, _, hasValue := strings.Cut(strings.TrimPrefix(token, "--"), "=")
			if SensitiveKey(key) {
				if hasValue {
					replace(i, "--"+key+"=<redacted>")
				} else {
					hideNext = true
				}
				continue
			}
		}
		if token == "-p" || token == "-P" {
			hideNext = true
			continue
		}
		if len(token) > 2 && (strings.HasPrefix(token, "-p") || strings.HasPrefix(token, "-P")) {
			replace(i, token[:2]+"<redacted>")
			continue
		}
		masked, _ := MaskText(arg, "<redacted>")
		replace(i, masked)
	}
	return out, count
}

// credentialWords recognizes quotes and escapes solely to locate credential
// switches. It never expands variables, substitutions, globs, or shell code.
func credentialWords(s string) ([]string, bool) {
	var out []string
	var word strings.Builder
	quote := byte(0)
	started := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
				started = true
				continue
			}
			if ch == '\\' && quote == '"' && i+1 < len(s) {
				i++
				word.WriteByte(s[i])
				continue
			}
			word.WriteByte(ch)
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
			started = true
		case '\\':
			if i+1 >= len(s) {
				return nil, false
			}
			i++
			word.WriteByte(s[i])
			started = true
		case ' ', '\t', '\r', '\n':
			if started {
				out = append(out, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteByte(ch)
			started = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	if started {
		out = append(out, word.String())
	}
	return out, true
}
