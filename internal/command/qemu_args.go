package command

import "strings"

// qemuImgCommand finds the subcommand after qemu-img's documented global
// options. qemu-img parses standard options before the command, so looking at
// args[0] misclassifies valid read-only invocations such as
// "qemu-img -T enable=info info disk.qcow2".
func qemuImgCommand(args []string) (name string, informational, known bool) {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			if index+1 >= len(args) {
				return "", false, false
			}
			name = args[index+1]
			return name, false, qemuImgKnown(name)
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			return arg, false, qemuImgKnown(arg)
		}
		if strings.HasPrefix(arg, "--") {
			option, _, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
			switch option {
			case "help":
				return "help", true, true
			case "version":
				return "version", true, true
			case "trace":
				if !hasValue {
					index++
					if index >= len(args) {
						return "", false, false
					}
				}
			default:
				return "", false, false
			}
			continue
		}

		rest := strings.TrimPrefix(arg, "-")
		for offset := 0; offset < len(rest); offset++ {
			switch rest[offset] {
			case 'h':
				return "help", true, true
			case 'V':
				return "version", true, true
			case 'T':
				if offset == len(rest)-1 {
					index++
					if index >= len(args) {
						return "", false, false
					}
				}
				offset = len(rest)
			default:
				return "", false, false
			}
		}
	}
	return "", false, false
}

// qemuNbdMode scans qemu-nbd options and returns its effective mode. Unlike
// qemu-img, qemu-nbd has no subcommand: -L/--list can appear before or after
// other options. Values are consumed explicitly so an option argument such as
// "--description --list" is not mistaken for list mode.
func qemuNbdMode(args []string) (mode string, known bool) {
	mode = "serve"
	known = true
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			continue
		}
		if strings.HasPrefix(arg, "--") {
			option, _, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
			matched, takesValue, matchedKnown := matchQemuNbdLongOption(option)
			if !matchedKnown {
				return mode, false
			}
			switch matched {
			case "help":
				return "help", true
			case "version":
				return "version", true
			case "list":
				if hasValue {
					known = false
					continue
				}
				mode = "list"
			default:
				if !takesValue && hasValue {
					known = false
					continue
				}
				if takesValue && !hasValue {
					index++
					if index >= len(args) {
						return mode, false
					}
				}
			}
			continue
		}

		rest := strings.TrimPrefix(arg, "-")
		if rest == "" {
			continue
		}
		for offset := 0; offset < len(rest); offset++ {
			switch rest[offset] {
			case 'h':
				return "help", true
			case 'V':
				return "version", true
			case 'L':
				mode = "list"
			case 't', 'v', 'A', 'd', 'r', 's', 'n':
				// No argument.
			case 'p', 'b', 'k', 'e', 'x', 'D', 'o', 'B', 'T', 'l', 'c', 'f':
				if offset == len(rest)-1 {
					index++
					if index >= len(args) {
						return mode, false
					}
				}
				offset = len(rest)
			default:
				return mode, false
			}
		}
	}
	return mode, known
}

func matchQemuNbdLongOption(name string) (option string, takesValue, known bool) {
	if name == "" {
		return "", false, false
	}
	match := ""
	for candidate, candidateTakesValue := range qemuNbdLongOptions {
		if candidate == name {
			return candidate, candidateTakesValue, true
		}
		if strings.HasPrefix(candidate, name) {
			if match != "" && match != candidate {
				return "", false, false
			}
			match = candidate
			takesValue = candidateTakesValue
		}
	}
	if match == "" {
		return "", false, false
	}
	return match, takesValue, true
}

var qemuNbdLongOptions = map[string]bool{
	"help": false, "version": false, "port": true, "bind": true, "socket": true,
	"shared": true, "persistent": false, "verbose": false, "export-name": true,
	"description": true, "offset": true, "allocation-depth": false, "bitmap": true,
	"list": false, "object": true, "tls-creds": true, "tls-authz": true,
	"tls-hostname": true, "handshake-limit": true,
	"trace": true, "fork": false, "pid-file": true, "selinux-label": true,
	"connect": true, "disconnect": false, "format": true, "read-only": false,
	"snapshot": false, "load-snapshot": true, "nocache": false, "cache": true,
	"aio": true, "discard": true, "detect-zeroes": true, "image-opts": false,
}

// qemuTraceSTAPCommand finds list/run after the tool's global -v option.
func qemuTraceSTAPCommand(args []string) (name string, informational, known bool) {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			if index+1 >= len(args) {
				return "", false, false
			}
			name = args[index+1]
			return name, false, name == "list" || name == "run"
		}
		switch arg {
		case "-h", "--help":
			return "help", true, true
		case "-V", "--version":
			return "version", true, true
		case "-v", "--verbose", "-vv", "-vvv":
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", false, false
		}
		return arg, false, arg == "list" || arg == "run"
	}
	return "", false, false
}
