package web

import (
	"strings"
	"unicode"

	"webvirtcloud-go/internal/store"
)

// failureTarget counts recent failures that name the same VM or storage
// object. It never retries a job.
type failureTarget struct {
	Name  string
	Count int
	Host  string
}

func repeatedFailureTargets(jobs []store.Job) []failureTarget {
	order := []string{}
	counts := map[string]*failureTarget{}
	for _, job := range jobs {
		if job.Status != "failed" {
			continue
		}
		name := failureTargetName(job.Label)
		if name == "" {
			continue
		}
		item := counts[name]
		if item == nil {
			item = &failureTarget{Name: name, Host: job.HostName}
			counts[name] = item
			order = append(order, name)
		}
		item.Count++
	}
	var out []failureTarget
	for _, name := range order {
		if counts[name].Count >= 2 {
			out = append(out, *counts[name])
		}
	}
	return out
}

func failureTargetName(label string) string {
	label = strings.TrimSpace(label)
	if label == "" || strings.Contains(label, "敏感参数已隐藏") {
		return ""
	}
	if name, rest, ok := strings.Cut(label, ":"); ok {
		name = strings.TrimSpace(name)
		if validTargetToken(name) && strings.TrimSpace(rest) != "" {
			return name
		}
	}
	fields := strings.Fields(label)
	for i := 0; i < len(fields)-1; i++ {
		if isTargetAction(fields[i]) && validTargetToken(fields[i+1]) {
			return fields[i+1]
		}
	}
	return ""
}

func isTargetAction(word string) bool {
	switch word {
	case "创建", "删除", "扩容", "缩容", "克隆", "导出", "导入", "启动", "停止", "迁移", "疏散", "故障重启", "定时检查点":
		return true
	default:
		return strings.HasSuffix(word, "卷") || strings.HasSuffix(word, "池") || strings.HasSuffix(word, "网络")
	}
}

func validTargetToken(value string) bool {
	if value == "" || len([]rune(value)) > 128 {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-_./", r) {
			continue
		}
		return false
	}
	return true
}
