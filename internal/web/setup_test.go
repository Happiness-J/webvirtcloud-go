package web

import (
	"testing"

	"webvirtcloud-go/internal/command"
)

func TestLocalCheckState(t *testing.T) {
	if checkState(true) != "可用" || checkState(false) != "未检测到" {
		t.Fatal(checkState(true))
	}
	if !setupNeeded(0) || setupNeeded(2) {
		t.Fatal("setup visibility")
	}
}

func TestSetupNetworkXML(t *testing.T) {
	xmlText, err := command.NetworkXML(command.NetworkDefinition{Name: "default", Mode: "nat", Address: "192.0.2.0", Prefix: 24})
	if err != nil || command.ValidateNetworkXML(xmlText) != nil {
		t.Fatal(err)
	}
}
