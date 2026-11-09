package command

import (
	"fmt"
	"strings"
)

// ParseArgs converts a human-entered command line into an argv slice. It is a
// parser, not a shell: expansion, pipes, redirection and command substitution
// are never performed.
func ParseArgs(line string) ([]string, error) {
	var args []string
	var current strings.Builder
	var quote rune
	escaped := false
	inToken := false

	for _, r := range line {
		if r == 0 {
			return nil, fmt.Errorf("参数包含 NUL 字节")
		}
		if escaped {
			current.WriteRune(r)
			inToken = true
			escaped = false
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				inToken = true
				continue
			}
			if r == '\\' && quote == '"' {
				escaped = true
				continue
			}
			current.WriteRune(r)
			inToken = true
			continue
		}
		switch {
		case r == '\\':
			escaped = true
			inToken = true
		case r == '\'' || r == '"':
			quote = r
			inToken = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inToken {
				args = append(args, current.String())
				current.Reset()
				inToken = false
			}
		default:
			current.WriteRune(r)
			inToken = true
		}
	}
	if escaped {
		return nil, fmt.Errorf("参数以反斜杠结尾")
	}
	if quote != 0 {
		return nil, fmt.Errorf("引号未闭合")
	}
	if inToken {
		args = append(args, current.String())
	}
	return args, nil
}
