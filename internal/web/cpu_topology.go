package web

import "fmt"

func cpuTopologyArg(vcpus, sockets, cores, threads int) (string, error) {
	if sockets < 1 || cores < 1 || threads < 1 || sockets > 128 || cores > 128 || threads > 16 {
		return "", fmt.Errorf("CPU 拓扑超出支持范围")
	}
	if sockets*cores*threads != vcpus {
		return "", fmt.Errorf("CPU 拓扑与 vCPU 总数不一致")
	}
	return fmt.Sprintf("%d,sockets=%d,cores=%d,threads=%d", vcpus, sockets, cores, threads), nil
}
