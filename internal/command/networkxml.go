package command

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

var bridgeNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,14}$`)

// NetworkDefinition is the narrow set of libvirt networks the panel creates.
type NetworkDefinition struct {
	Name    string
	Mode    string
	Bridge  string
	Address string
	Prefix  int
}

// NetworkXML returns a persistent libvirt network definition for NAT, an
// existing host bridge, or an isolated network with no forwarding.
func NetworkXML(def NetworkDefinition) (string, error) {
	if err := validateActionOperand(def.Name); err != nil {
		return "", fmt.Errorf("虚拟网络名称无效")
	}
	prefix := ""
	if def.Mode != "bridge" {
		ip, err := netip.ParseAddr(def.Address)
		if err != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() {
			return "", fmt.Errorf("网络地址无效")
		}
		if def.Prefix < 8 || def.Prefix > 29 {
			return "", fmt.Errorf("网络前缀无效")
		}
		network := netip.PrefixFrom(ip, def.Prefix).Masked()
		if network.Addr() != ip {
			return "", fmt.Errorf("请填写网络地址，而不是网段中的主机地址")
		}
		start, end := dhcpRange(network)
		prefix = fmt.Sprintf("<ip address='%s' prefix='%d'><dhcp><range start='%s' end='%s'/></dhcp></ip>", ip, def.Prefix, start, end)
	}
	var body string
	switch def.Mode {
	case "nat":
		body = "<forward mode='nat'/>" + prefix
	case "isolated":
		body = prefix
	case "bridge":
		if !bridgeNamePattern.MatchString(def.Bridge) {
			return "", fmt.Errorf("桥接名称无效")
		}
		body = fmt.Sprintf("<forward mode='bridge'/><bridge name='%s'/>", def.Bridge)
	default:
		return "", fmt.Errorf("网络模式无效")
	}
	return fmt.Sprintf("<network><name>%s</name>%s</network>", def.Name, body), nil
}

func dhcpRange(network netip.Prefix) (string, string) {
	base := network.Addr()
	last := network.Masked().Addr()
	for i := 0; i < (1<<(32-network.Bits()))-1; i++ {
		last = last.Next()
	}
	start := base
	for i := 0; i < 10 && start != last; i++ {
		start = start.Next()
	}
	end := last.Prev()
	if !start.Less(end) {
		start = base.Next()
	}
	return start.String(), end.String()
}

var networkXMLPattern = regexp.MustCompile(`^<network><name>[A-Za-z0-9][A-Za-z0-9._-]{0,127}</name>(<forward mode='nat'/>|<forward mode='bridge'/><bridge name='[A-Za-z][A-Za-z0-9_-]{0,14}'/>|)(<ip address='(?:\d{1,3}\.){3}\d{1,3}' prefix='(?:[8-9]|[12][0-9])'><dhcp><range start='(?:\d{1,3}\.){3}\d{1,3}' end='(?:\d{1,3}\.){3}\d{1,3}'/></dhcp></ip>|)</network>$`)

// ValidateNetworkXML accepts only definitions produced by NetworkXML.
func ValidateNetworkXML(value string) error {
	if !networkXMLPattern.MatchString(value) || strings.Contains(value, "..") {
		return fmt.Errorf("虚拟网络定义无效")
	}
	if !strings.Contains(value, "<ip ") {
		return nil
	}
	address := between(value, "address='", "'")
	prefixText := between(value, "prefix='", "'")
	start := between(value, "start='", "'")
	end := between(value, "end='", "'")
	prefix, err := strconv.Atoi(prefixText)
	ip, ipErr := netip.ParseAddr(address)
	if err != nil || ipErr != nil || prefix < 8 || prefix > 29 || !ip.Is4() {
		return fmt.Errorf("虚拟网络定义无效")
	}
	network := netip.PrefixFrom(ip, prefix).Masked()
	if network.Addr() != ip {
		return fmt.Errorf("虚拟网络定义无效")
	}
	wantStart, wantEnd := dhcpRange(network)
	if start != wantStart || end != wantEnd {
		return fmt.Errorf("虚拟网络定义无效")
	}
	return nil
}

func between(value, start, end string) string {
	i := strings.Index(value, start)
	if i < 0 {
		return ""
	}
	value = value[i+len(start):]
	j := strings.Index(value, end)
	if j < 0 {
		return ""
	}
	return value[:j]
}
