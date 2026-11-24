package web

import (
	"strings"
	"unicode/utf8"

	"webvirtcloud-go/internal/store"
)

// failureCategory is a read-only grouping of an already stored job error.
// It never retries or runs the job.
type failureCategory struct {
	Key      string
	Label    string
	Hint     string
	Count    int
	Examples []failureExample
}

type failureExample struct {
	ID    string
	Label string
	Error string
}

func failureGroups(jobs []store.Job) []failureCategory {
	order := []failureCategory{
		{Key: "name", Label: "名称或参数无效", Hint: "名称、规格、地址或参数没通过校验。先改输入再重新提交。"},
		{Key: "notfound", Label: "资源不存在", Hint: "主机、虚拟机、存储或镜像已经不在。确认目标还在后再操作。"},
		{Key: "storage", Label: "存储空间或镜像", Hint: "磁盘、存储池或镜像库写失败。先看剩余空间和镜像是否完整。"},
		{Key: "connect", Label: "主机连接失败", Hint: "面板没能连上 libvirt。先看主机是否可达、连接是否仍有效。"},
		{Key: "permission", Label: "权限不足", Hint: "账号或宿主机权限不够。确认授权后再重试。"},
		{Key: "timeout", Label: "执行超时", Hint: "命令没有在时限内完成。先看宿主机负载，再决定要不要重做。"},
		{Key: "conflict", Label: "资源冲突", Hint: "同名资源或正在进行的操作挡住了这一步。先处理占用再提交。"},
		{Key: "other", Label: "其他失败", Hint: "现有类别没有覆盖。打开任务详情看原始错误。"},
	}
	index := map[string]int{}
	for i := range order {
		index[order[i].Key] = i
	}
	for _, job := range jobs {
		if job.Status != "failed" {
			continue
		}
		key := classifyFailure(job.Error)
		item := &order[index[key]]
		item.Count++
		if len(item.Examples) >= 3 {
			continue
		}
		text := strings.TrimSpace(job.Error)
		if text == "" {
			text = "失败原因已隐藏或未记录"
		}
		item.Examples = append(item.Examples, failureExample{ID: job.ID, Label: job.Label, Error: clipFailure(text, 160)})
	}
	out := make([]failureCategory, 0, len(order))
	for _, item := range order {
		if item.Count > 0 {
			out = append(out, item)
		}
	}
	return out
}

func classifyFailure(text string) string {
	value := strings.ToLower(strings.TrimSpace(text))
	switch {
	case value == "":
		return "other"
	case strings.Contains(value, "超时") || strings.Contains(value, "timeout") || strings.Contains(value, "timed out"):
		return "timeout"
	case strings.Contains(value, "权限") || strings.Contains(value, "permission") || strings.Contains(value, "forbidden") || strings.Contains(value, "denied"):
		return "permission"
	case strings.Contains(value, "不存在") || strings.Contains(value, "not found") || strings.Contains(value, "no such"):
		return "notfound"
	case strings.Contains(value, "空间") || strings.Contains(value, "镜像") || strings.Contains(value, "存储") || strings.Contains(value, "no space") || strings.Contains(value, "disk"):
		return "storage"
	case strings.Contains(value, "连接") || strings.Contains(value, "decrypt") || strings.Contains(value, "解密") || strings.Contains(value, "unreachable") || strings.Contains(value, "connection"):
		return "connect"
	case strings.Contains(value, "已存在") || strings.Contains(value, "冲突") || strings.Contains(value, "already exists") || strings.Contains(value, "busy") || strings.Contains(value, "lock"):
		return "conflict"
	case strings.Contains(value, "无效") || strings.Contains(value, "invalid") || strings.Contains(value, "校验"):
		return "name"
	default:
		return "other"
	}
}

func clipFailure(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}
