package web

import (
	"fmt"
	"strconv"
	"strings"

	"webvirtcloud-go/internal/command"
)

func nicQueueArgs(vm, mac string, queues int) ([]string, error) {
	if err := command.ValidateActionOperandPublic(vm); err != nil || !validMAC(mac) {
		return nil, fmt.Errorf("网卡参数无效")
	}
	if queues < 1 || queues > 16 {
		return nil, fmt.Errorf("队列数必须在 1 到 16 之间")
	}
	return []string{vm, "--edit", "mac=" + mac, "--network", "driver_queues=" + strconv.Itoa(queues)}, nil
}

func diskTuneArgs(vm, target, readBytes, writeBytes, readIOPS, writeIOPS string) ([]string, error) {
	if err := command.ValidateActionOperandPublic(vm); err != nil || command.ValidateDiskTargetPublic(target) != nil {
		return nil, fmt.Errorf("磁盘参数无效")
	}
	values := []string{readBytes, writeBytes, readIOPS, writeIOPS}
	for _, value := range values {
		if !validTuneValue(value) {
			return nil, fmt.Errorf("I/O 限速必须是 0 到 1000000000000 的整数")
		}
	}
	return []string{"blkdeviotune", vm, target, "--read-bytes-sec", readBytes, "--write-bytes-sec", writeBytes, "--read-iops-sec", readIOPS, "--write-iops-sec", writeIOPS, "--config"}, nil
}

func diskDiscardArgs(vm, target, discard string) ([]string, error) {
	if err := command.ValidateActionOperandPublic(vm); err != nil || command.ValidateDiskTargetPublic(target) != nil {
		return nil, fmt.Errorf("磁盘参数无效")
	}
	if discard != "ignore" && discard != "unmap" {
		return nil, fmt.Errorf("discard 模式无效")
	}
	return []string{vm, "--edit", "target=" + target, "--disk", "discard=" + discard}, nil
}

func validTuneValue(value string) bool {
	number, err := strconv.ParseUint(value, 10, 64)
	return err == nil && number <= 1000000000000
}

func validMAC(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 6 {
		return false
	}
	for _, part := range parts {
		if len(part) != 2 {
			return false
		}
		for _, r := range part {
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
				return false
			}
		}
	}
	return true
}

type nicChoice struct{ MAC, Label string }

func parseNICChoices(output string) []nicChoice {
	var out []nicChoice
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !validMAC(fields[4]) {
			continue
		}
		out = append(out, nicChoice{MAC: strings.ToLower(fields[4]), Label: fields[1] + " · " + fields[2]})
	}
	return out
}
