package appmon

import (
	"path/filepath"
	"strings"
)

// parseDesktopEntry reads the [Desktop Entry] group; group keys from other
// sections (Desktop Action …) are ignored deliberately.
func parseDesktopEntry(content string) map[string]string {
	out := map[string]string{}
	inGroup := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inGroup = line == "[Desktop Entry]"
			continue
		}
		if !inGroup {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		// Localised overrides (Name[zh_CN]) are skipped; plain keys only.
		if strings.Contains(key, "[") {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	return out
}

func isTrue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes":
		return true
	}
	return false
}

// execBase extracts the executable basename from a Desktop Entry Exec line.
// The line may use env prefixes, shell wrappers and quoted paths with spaces.
func execBase(exec string) string {
	tokens := splitExec(exec)
	i := 0
	for i < len(tokens) {
		base := filepath.Base(tokens[i])
		if base == "env" {
			i++
			for i < len(tokens) && strings.Contains(tokens[i], "=") {
				i++
			}
			continue
		}
		if strings.HasPrefix(tokens[i], "-") || isAssignment(tokens[i]) {
			i++
			continue
		}
		break
	}
	if i >= len(tokens) {
		return ""
	}
	base := filepath.Base(tokens[i])
	switch base {
	case "sh", "bash", "dash", "zsh":
		// `sh -c "<command>"` hides the real launcher in the next token.
		if i+2 < len(tokens) && tokens[i+1] == "-c" {
			return filepath.Base(tokens[i+2])
		}
	}
	return base
}

func isAssignment(token string) bool {
	return strings.Contains(token, "=") && !strings.Contains(token, "/")
}

// splitExec splits a command line on whitespace while keeping quoted segments,
// so paths with spaces survive as one token.
func splitExec(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// linuxIconPath resolves an Icon= value to a PNG file. Theme directories are
// probed from the largest size down so the tile stays crisp.
