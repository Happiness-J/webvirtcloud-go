package web

import (
	"encoding/xml"
	"fmt"

	"webvirtcloud-go/internal/command"
)

type guestDeviceState struct {
	RNG      bool
	Watchdog string
}

func parseGuestDevices(xmlText string) guestDeviceState {
	var parsed struct {
		Devices struct {
			RNGs []struct {
				Model string `xml:"model,attr"`
			} `xml:"rng"`
			Watchdogs []struct {
				Action string `xml:"action,attr"`
			} `xml:"watchdog"`
		} `xml:"devices"`
	}
	if err := xml.Unmarshal([]byte(xmlText), &parsed); err != nil {
		return guestDeviceState{}
	}
	state := guestDeviceState{RNG: len(parsed.Devices.RNGs) > 0}
	if len(parsed.Devices.Watchdogs) > 0 {
		state.Watchdog = parsed.Devices.Watchdogs[0].Action
		if state.Watchdog == "" {
			state.Watchdog = "reset"
		}
	}
	return state
}

func rngArgs(vm string, add bool) ([]string, error) {
	if err := command.ValidateActionOperandPublic(vm); err != nil {
		return nil, fmt.Errorf("虚拟机名称无效")
	}
	if add {
		return []string{vm, "--add-device", "--rng", "/dev/urandom"}, nil
	}
	return []string{vm, "--remove-device", "--rng", "1"}, nil
}

func watchdogArgs(vm, action string, add bool) ([]string, error) {
	if err := command.ValidateActionOperandPublic(vm); err != nil || !validWatchdogAction(action) {
		return nil, fmt.Errorf("看门狗参数无效")
	}
	if add {
		return []string{vm, "--add-device", "--watchdog", "i6300esb,action=" + action}, nil
	}
	return []string{vm, "--remove-device", "--watchdog", "1"}, nil
}

func validWatchdogAction(value string) bool {
	return value == "reset" || value == "shutdown" || value == "none"
}

func watchdogLabel(action string) string {
	switch action {
	case "reset":
		return "超时后重启"
	case "shutdown":
		return "超时后关机"
	case "none":
		return "超时后仅告警"
	default:
		return "未配置"
	}
}
