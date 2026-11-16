package web

import (
	"encoding/xml"
	"fmt"
	"strconv"
)

type videoDevice struct {
	Model string
	VRAM  int
}

func videoArg(model string, vram int, graphics string) (string, error) {
	switch model {
	case "", "virtio":
		model = "virtio"
	case "qxl", "vga", "none":
	default:
		return "", fmt.Errorf("显卡型号无效")
	}
	if model == "none" {
		if graphics != "none" {
			return "", fmt.Errorf("无头显卡只能与无图形控制台一起使用")
		}
		return "model=none", nil
	}
	if graphics == "none" {
		return "", fmt.Errorf("选择图形控制台时必须配置显卡")
	}
	if vram < 16 || vram > 512 || vram%16 != 0 {
		return "", fmt.Errorf("显存必须是 16 到 512 MiB 之间的 16 倍数")
	}
	return fmt.Sprintf("model=%s,vram=%d", model, vram*1024), nil
}

func videoEditArgs(vm, model string, vram int, graphics string) ([]string, error) {
	value, err := videoArg(model, vram, graphics)
	if err != nil {
		return nil, err
	}
	return []string{vm, "--edit", "--video", value}, nil
}

func parseVideo(xmlText string) videoDevice {
	var parsed struct {
		Devices struct {
			Videos []struct {
				Model struct {
					Type string `xml:"type,attr"`
					VRAM int    `xml:"vram,attr"`
				} `xml:"model"`
			} `xml:"video"`
		} `xml:"devices"`
	}
	if err := xml.Unmarshal([]byte(xmlText), &parsed); err != nil || len(parsed.Devices.Videos) == 0 {
		return videoDevice{}
	}
	item := parsed.Devices.Videos[0]
	return videoDevice{Model: item.Model.Type, VRAM: item.Model.VRAM / 1024}
}

func videoVRAM(value string) int {
	number, _ := strconv.Atoi(value)
	if number == 0 {
		return 64
	}
	return number
}
