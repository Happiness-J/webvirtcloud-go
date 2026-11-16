package web

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

type gdbStub struct {
	Enabled bool
	Port    int
}

func gdbPort(value string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1024 || port > 65535 {
		return 0, fmt.Errorf("GDB 端口必须在 1024 到 65535 之间")
	}
	return port, nil
}

func gdbCommand(port int) string {
	return fmt.Sprintf("-gdb tcp:127.0.0.1:%d", port)
}

func gdbEnableArgs(vm string, port int) []string {
	return []string{vm, "--edit", "--qemu-commandline", gdbCommand(port)}
}

func parseGDB(xmlText string) gdbStub {
	var parsed struct {
		Commandline struct {
			Args []struct {
				Value string `xml:"value,attr"`
			} `xml:"arg"`
		} `xml:"commandline"`
	}
	if err := xml.Unmarshal([]byte(xmlText), &parsed); err != nil {
		return gdbStub{}
	}
	for _, arg := range parsed.Commandline.Args {
		value, ok := strings.CutPrefix(strings.TrimPrefix(arg.Value, "-"), "gdb tcp:127.0.0.1:")
		if !ok {
			continue
		}
		port, err := gdbPort(value)
		if err == nil {
			return gdbStub{Enabled: true, Port: port}
		}
	}
	return gdbStub{}
}

func gdbStartArgs(vm string) []string {
	return []string{"start", vm, "--paused"}
}
