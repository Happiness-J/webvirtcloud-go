package command

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

var poolSourcePattern = regexp.MustCompile(`^[A-Za-z0-9_.:/@-]{1,200}$`)

// StoragePoolXML returns a persistent libvirt pool definition for a directory,
// NFS export, iSCSI target, or LVM volume group.
func StoragePoolXML(kind, name, source, target string) (string, error) {
	if err := validateActionOperand(name); err != nil {
		return "", fmt.Errorf("存储池名称无效")
	}
	if !validPoolSource(source) {
		return "", fmt.Errorf("存储池来源无效")
	}
	var xmlText string
	switch kind {
	case "dir":
		if !validPoolTarget(target) {
			return "", fmt.Errorf("目录路径无效")
		}
		xmlText = fmt.Sprintf("<pool type='dir'><name>%s</name><target><path>%s</path></target></pool>", name, xmlAttr(target))
	case "netfs":
		host, export, ok := strings.Cut(source, ":")
		if !ok || net.ParseIP(host) == nil || !strings.HasPrefix(export, "/") || strings.Contains(export, "..") || !validPoolTarget(target) {
			return "", fmt.Errorf("NFS 来源必须是地址:/导出路径")
		}
		xmlText = fmt.Sprintf("<pool type='netfs'><name>%s</name><source><host name='%s'/><dir path='%s'/></source><target><path>%s</path></target></pool>", name, xmlAttr(host), xmlAttr(export), xmlAttr(target))
	case "iscsi":
		host, iqn, ok := strings.Cut(source, "/")
		if !ok || net.ParseIP(host) == nil || !strings.HasPrefix(iqn, "iqn.") {
			return "", fmt.Errorf("iSCSI 来源必须是地址/iqn")
		}
		xmlText = fmt.Sprintf("<pool type='iscsi'><name>%s</name><source><host name='%s'/><device path='%s'/></source></pool>", name, xmlAttr(host), xmlAttr(iqn))
	case "logical":
		if strings.Contains(source, "/") {
			return "", fmt.Errorf("LVM 卷组名称无效")
		}
		xmlText = fmt.Sprintf("<pool type='logical'><name>%s</name><source><name>%s</name></source></pool>", name, xmlAttr(source))
	default:
		return "", fmt.Errorf("存储池类型无效")
	}
	if err := ValidateStoragePoolXML(xmlText); err != nil {
		return "", err
	}
	return xmlText, nil
}

func validPoolSource(value string) bool {
	return poolSourcePattern.MatchString(value) && !strings.Contains(value, "..")
}

func validPoolTarget(value string) bool {
	return strings.HasPrefix(value, "/") && !strings.Contains(value, "..") && !strings.ContainsAny(value, " \t\r\n")
}

func xmlAttr(value string) string {
	return strings.NewReplacer("&", "&amp;", "'", "&apos;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(value)
}

var poolXMLPattern = regexp.MustCompile(`^<pool type='dir'><name>[A-Za-z0-9][A-Za-z0-9._-]{0,127}</name><target><path>/[A-Za-z0-9_./-]{1,200}</path></target></pool>$|^<pool type='netfs'><name>[A-Za-z0-9][A-Za-z0-9._-]{0,127}</name><source><host name='[0-9a-fA-F:.]+'/><dir path='/[A-Za-z0-9_./-]{1,200}'/></source><target><path>/[A-Za-z0-9_./-]{1,200}</path></target></pool>$|^<pool type='iscsi'><name>[A-Za-z0-9][A-Za-z0-9._-]{0,127}</name><source><host name='[0-9a-fA-F:.]+'/><device path='iqn[.][A-Za-z0-9_.:-]{1,180}'/></source></pool>$|^<pool type='logical'><name>[A-Za-z0-9][A-Za-z0-9._-]{0,127}</name><source><name>[A-Za-z0-9_.-]{1,80}</name></source></pool>$`)

// ValidateStoragePoolXML accepts only definitions produced by StoragePoolXML.
func ValidateStoragePoolXML(value string) error {
	if !poolXMLPattern.MatchString(value) || strings.Contains(value, "..") || strings.ContainsAny(value, "\n\r\x00") {
		return fmt.Errorf("存储池定义无效")
	}
	return nil
}
