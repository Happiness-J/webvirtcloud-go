package command

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

var syntaxPunctuation = map[string]bool{
	"[": true, "]": true, "{": true, "}": true, "(": true, ")": true,
	"|": true, "...": true,
}

var syntaxMeta = map[string]bool{
	"or": true, "and": true, "to": true, "of": true, "a": true, "an": true,
}

// AnalyzeSyntax turns the official synopsis into form fields. It deliberately
// models only flags and operands; mutual-exclusion and nested grammar remain
// visible in the usage text, while the server remains authoritative for argv.
func AnalyzeSyntax(commandName, syntax string) ([]ParameterSpec, []ParameterSpec) {
	tokens := tokenizeSyntax(syntax)
	// The first token of a full synopsis is the command name, which can differ
	// from the catalog key when libvirt documents an alias (for example
	// find-pool-sources-as / find-storage-pool-sources-as).
	if commandName != "" && len(tokens) > 0 && !isOption(tokens[0]) &&
		tokens[0] != "[" && tokens[0] != "{" && tokens[0] != "(" && tokens[0] != "--" {
		tokens = tokens[1:]
	}
	options := make([]ParameterSpec, 0, 8)
	positionals := make([]ParameterSpec, 0, 4)
	seenOptions := make(map[string]bool)
	seenPositionals := make(map[string]bool)
	consumed := make(map[int]bool)
	squareDepth := 0

	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		switch token {
		case "[":
			squareDepth++
			continue
		case "]":
			if squareDepth > 0 {
				squareDepth--
			}
			continue
		}
		if !isOption(token) {
			continue
		}
		flags, inlineValue, hasInline := splitOption(token)
		for _, flag := range expandShortFlags(flags) {
			if seenOptions[flag] {
				continue
			}
			spec := ParameterSpec{Name: flag, Flag: flag, Label: flag, ValueType: "bool"}
			value := inlineValue
			hasValue := hasInline
			if hasInline && value == "" && i+1 < len(tokens) && tokens[i+1] == "[" {
				if choices := bracketChoices(tokens, i+1, consumed); len(choices) > 0 {
					value = choices[0]
					spec.Choices = choices
					spec.ValueType = "choice"
					spec.Placeholder = choices[0]
					spec.Label = flag + " " + choices[0]
				}
			}
			if !hasInline && i+1 < len(tokens) && tokens[i+1] == "[" {
				if choices := bracketChoices(tokens, i+1, consumed); len(choices) > 0 {
					value = choices[0]
					hasValue = true
					spec.OptionalValue = true
					spec.Choices = choices
					spec.ValueType = "choice"
					spec.Placeholder = choices[0]
					spec.Label = flag + " [" + strings.Join(choices, " | ") + "]"
				}
			}
			if !hasInline && flag == "--new-desc" && i+1 < len(tokens) && canBeOptionValue(tokens[i+1]) {
				// The manual writes this option's value as an English prose
				// description: "--new-desc New description or title message".
				// Consume the prose as one form value instead of exposing its
				// words as bogus positional arguments.
				start := i + 1
				end := start
				for end < len(tokens) && tokens[end] != "]" && tokens[end] != "|" && tokens[end] != "}" {
					end++
				}
				if end > start {
					value = strings.Join(tokens[start:end], " ")
					hasValue = true
					for j := start; j < end; j++ {
						consumed[j] = true
					}
				}
			}
			if !hasInline && i+1 < len(tokens) && canBeOptionValue(tokens[i+1]) {
				value = tokens[i+1]
				hasValue = true
				consumed[i+1] = true
				if choices := collectChoices(tokens, i+1, consumed); len(choices) > 1 {
					spec.Choices = choices
					spec.ValueType = "choice"
				}
			}
			if hasValue {
				spec.Placeholder = value
				spec.Label = flag + " " + value
				if flag == "--new-desc" {
					spec.Label = "新描述/标题"
					spec.Placeholder = "新的描述或标题文本"
					spec.ValueType = "text"
				}
				if spec.ValueType != "choice" {
					spec.ValueType = valueType(value)
				}
				if i+2 < len(tokens) && tokens[i+2] == "..." {
					spec.Placeholder += "..."
					spec.Label += "..."
					consumed[i+2] = true
				}
			}
			spec.Required = squareDepth == 0 && !inAlternativeGroup(tokens, i)
			seenOptions[flag] = true
			options = append(options, spec)
		}
	}

	squareDepth = 0
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		switch token {
		case "[":
			squareDepth++
			continue
		case "]":
			if squareDepth > 0 {
				squareDepth--
			}
			continue
		}
		if consumed[i] || syntaxPunctuation[token] || syntaxMeta[strings.ToLower(token)] || isOption(token) {
			continue
		}
		if token == "--" {
			positionals = append(positionals, ParameterSpec{
				Name: "--", Label: "参数分隔符", Literal: "--", ValueType: "literal",
			})
			continue
		}
		if token == commandName && i == 0 {
			continue
		}
		if token == "" || strings.ContainsAny(token, "|{}()") {
			continue
		}
		placeholder := strings.Trim(token, "<>,")
		if placeholder == "" || strings.HasPrefix(placeholder, "-") {
			continue
		}
		multiple := strings.HasSuffix(placeholder, "...") || strings.HasSuffix(placeholder, "..")
		if i+1 < len(tokens) && tokens[i+1] == "..." {
			multiple = true
			consumed[i+1] = true
		}
		// Some pages spell the variadic tail as "arg, ..." rather than
		// "arg...". Treat the comma-separated ellipsis as repeatability too.
		if i+2 < len(tokens) && tokens[i+1] == "," && tokens[i+2] == "..." {
			multiple = true
			consumed[i+2] = true
		}
		// Some upstream synopses write repeatability as "name [...]" rather
		// than "name...". Keep the field repeatable without swallowing the
		// enclosing optional group.
		if !multiple && i+3 < len(tokens) && tokens[i+1] == "[" && tokens[i+2] == "..." && tokens[i+3] == "]" {
			multiple = true
			consumed[i+1] = true
			consumed[i+2] = true
			consumed[i+3] = true
		}
		placeholder = strings.TrimSuffix(strings.TrimSuffix(placeholder, "..."), "..")
		name := placeholder
		if name == "" {
			continue
		}
		if strings.Contains(name, "=") {
			name = strings.TrimSpace(strings.SplitN(name, "=", 2)[0])
		}
		if seenPositionals[name] {
			if multiple {
				for j := range positionals {
					if positionals[j].Name == name {
						positionals[j].Multiple = true
						break
					}
				}
			}
			continue
		}
		kind := valueType(name)
		if strings.Contains(strings.ToLower(name), "xml") {
			kind = "textarea"
		}
		seenPositionals[name] = true
		positionals = append(positionals, ParameterSpec{
			Name:        name,
			Label:       positionalLabel(name),
			Placeholder: placeholder,
			ValueType:   kind,
			Required:    squareDepth == 0 && !inAlternativeGroup(tokens, i),
			Multiple:    multiple,
		})
	}
	return options, positionals
}

func tokenizeSyntax(syntax string) []string {
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	for i := 0; i < len(syntax); {
		if strings.HasPrefix(syntax[i:], "..") {
			flush()
			tokens = append(tokens, "...")
			for i < len(syntax) && syntax[i] == '.' {
				i++
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(syntax[i:])
		if unicode.IsSpace(r) {
			flush()
			i += size
			continue
		}
		if r == '*' {
			flush()
			i += size
			continue
		}
		if strings.ContainsRune("[]{}()|", r) {
			flush()
			tokens = append(tokens, string(r))
			i += size
			continue
		}
		current.WriteRune(r)
		i += size
	}
	flush()
	return tokens
}

func isOption(token string) bool {
	if token == "-" || token == "--" || !strings.HasPrefix(token, "-") {
		return false
	}
	if strings.HasPrefix(token, "--") {
		return len(token) > 2
	}
	return len(token) > 1 && ((token[1] >= 'A' && token[1] <= 'Z') || (token[1] >= 'a' && token[1] <= 'z'))
}

func splitOption(token string) (string, string, bool) {
	if name, value, ok := strings.Cut(token, "="); ok {
		return name, value, true
	}
	return token, "", false
}

func expandShortFlags(flag string) []string {
	if !strings.HasPrefix(flag, "-") || strings.HasPrefix(flag, "--") || len(flag) <= 2 {
		return []string{flag}
	}
	body := flag[1:]
	for _, r := range body {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return []string{flag}
		}
	}
	out := make([]string, 0, len(body))
	for _, r := range body {
		out = append(out, "-"+string(r))
	}
	return out
}

func canBeOptionValue(token string) bool {
	if token == "" || isOption(token) || syntaxPunctuation[token] {
		return false
	}
	if strings.HasPrefix(token, "[") || strings.HasPrefix(token, "{") || strings.HasPrefix(token, "(") {
		return false
	}
	return true
}

func collectChoices(tokens []string, start int, consumed map[int]bool) []string {
	if start >= len(tokens) || !canBeOptionValue(tokens[start]) {
		return nil
	}
	choices := []string{tokens[start]}
	for i := start + 1; i+1 < len(tokens); i += 2 {
		if tokens[i] != "|" || !canBeOptionValue(tokens[i+1]) {
			break
		}
		choices = append(choices, tokens[i+1])
		consumed[i] = true
		consumed[i+1] = true
	}
	return choices
}

func bracketChoices(tokens []string, start int, consumed map[int]bool) []string {
	if start >= len(tokens) || tokens[start] != "[" {
		return nil
	}
	choices := make([]string, 0, 4)
	valid := true
	for i := start + 1; i < len(tokens) && tokens[i] != "]"; i++ {
		if tokens[i] == "|" || syntaxPunctuation[tokens[i]] {
			continue
		}
		if isOption(tokens[i]) || syntaxMeta[strings.ToLower(tokens[i])] {
			// A bracket containing another flag is an optional sub-option,
			// not a value enum. Do not turn "--wait [--async]" into a choice.
			valid = false
			break
		}
		choices = append(choices, tokens[i])
	}
	if !valid || len(choices) == 0 {
		return nil
	}
	for i := start + 1; i < len(tokens) && tokens[i] != "]"; i++ {
		consumed[i] = true
	}
	return choices
}

func valueType(name string) string {
	lower := strings.ToLower(strings.Trim(name, "<>[],."))
	numberNames := map[string]bool{
		"n": true, "num": true, "count": true, "number": true, "seconds": true,
		"timeout": true, "port": true, "depth": true, "id": true, "index": true,
		"granularity": true, "buffer_size": true, "offset": true, "len": true,
		"milliseconds": true,
	}
	if numberNames[lower] {
		return "number"
	}
	return "text"
}

func positionalLabel(name string) string {
	lower := strings.ToLower(name)
	switch lower {
	case "domain", "domain-name-or-uuid", "domain-name", "domain-id", "domain-uuid":
		return "虚拟机（名称/ID/UUID）"
	case "name":
		return "名称"
	case "uuid":
		return "UUID"
	case "desturi", "migrateuri", "graphicsuri":
		return "目标 URI"
	case "file", "filename", "path", "source", "target":
		return "路径"
	case "filename1":
		return "输入镜像 1"
	case "filename2":
		return "额外输入镜像（可重复）"
	case "output_filename":
		return "输出镜像路径"
	case "bitmap":
		return "Bitmap 名称"
	case "pool", "pool-name", "pool-name-or-uuid":
		return "存储池"
	case "vol", "volume", "vol-name", "vol-name-or-key-or-path":
		return "存储卷"
	case "net", "network", "net-name", "net-name-or-uuid":
		return "虚拟网络"
	case "iface", "interface":
		return "网络接口"
	case "secret", "secret-uuid":
		return "Secret"
	case "size":
		return "大小（如 10G）"
	case "off", "offset":
		return "偏移"
	case "len", "length":
		return "长度"
	case "count":
		return "数量"
	case "pattern":
		return "数据模式"
	case "tag":
		return "断点标签"
	case "signal":
		return "信号"
	case "milliseconds":
		return "毫秒数"
	case "device", "dev":
		return "设备"
	case "format":
		return "格式"
	case "type":
		return "类型"
	case "action":
		return "动作"
	case "command", "command...":
		return "命令/参数"
	case "description":
		return "描述"
	case "uri":
		return "URI"
	case "key":
		return "Key"
	case "set":
		return "XML 内容（set 参数）"
	case "/path/to/binary":
		return "容器内二进制路径"
	case "arg1":
		return "命令参数 1"
	case "arg2":
		return "命令参数 2（可重复）"
	}
	return name
}

func inAlternativeGroup(tokens []string, index int) bool {
	depth := 0
	open := -1
	for i := index - 1; i >= 0; i-- {
		switch tokens[i] {
		case ")", "}":
			depth++
		case "(", "{":
			if depth == 0 {
				open = i
				i = -1
			} else {
				depth--
			}
		}
	}
	if open < 0 {
		return false
	}
	depth = 0
	for i := open; i < len(tokens); i++ {
		switch tokens[i] {
		case "(", "{":
			depth++
		case ")", "}":
			depth--
			if depth == 0 {
				return false
			}
		case "|":
			if depth == 1 {
				return true
			}
		}
	}
	return false
}
