package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type libraryImage struct {
	Name     string
	Path     string
	Kind     string
	Category string
	Size     string
	SHA256   string
}

func listLibraryImages(dir, query, category string) ([]libraryImage, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	category = strings.TrimSpace(category)
	out := make([]libraryImage, 0)
	for _, entry := range entries {
		if entry.IsDir() || !validLibraryName(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if !validInstallISO(dir, path) && !validLibraryDisk(dir, path) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		item := libraryImage{Name: entry.Name(), Path: path, Kind: libraryKind(entry.Name()), Category: libraryCategory(entry.Name()), Size: fmt.Sprintf("%d B", info.Size())}
		if category != "" && category != "all" && item.Category != category {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(item.Name), query) {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func libraryKind(name string) string {
	if strings.HasSuffix(strings.ToLower(name), ".iso") {
		return "ISO"
	}
	return "磁盘"
}

func libraryCategory(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "virtio"):
		return "virtio"
	case strings.Contains(lower, "guest-agent") || strings.Contains(lower, "guestagent"):
		return "guest-agent"
	case strings.HasSuffix(lower, ".iso"):
		return "installer"
	default:
		return "disk"
	}
}

func validLibraryName(name string) bool {
	if name == "" || len(name) > 128 || strings.Contains(name, "..") || strings.HasPrefix(name, ".") {
		return false
	}
	lower := strings.ToLower(name)
	if !(strings.HasSuffix(lower, ".iso") || strings.HasSuffix(lower, ".qcow2") || strings.HasSuffix(lower, ".raw") || strings.HasSuffix(lower, ".vmdk")) {
		return false
	}
	for _, r := range name {
		if !(r == '.' || r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

func validLibraryDisk(dir, path string) bool {
	lower := strings.ToLower(path)
	if !(strings.HasSuffix(lower, ".qcow2") || strings.HasSuffix(lower, ".raw") || strings.HasSuffix(lower, ".vmdk")) || strings.Contains(path, "..") {
		return false
	}
	root := filepath.Clean(dir)
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(root, clean)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func librarySHA256(dir, name string) (string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	file, err := root.Open(name) // #nosec G304 -- name is validated and opened relative to the image directory root.

	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func saveLibraryUpload(dir, name string, body io.Reader, limit int64) error {
	if !validLibraryName(name) {
		return fmt.Errorf("文件名只允许字母、数字、点和短横线")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("镜像目录不可用")
	}
	defer root.Close()
	file, err := root.Create(name)
	if err != nil {
		return fmt.Errorf("无法保存镜像")
	}
	defer file.Close()
	written, err := io.Copy(file, io.LimitReader(body, limit+1))
	if err != nil {
		return fmt.Errorf("保存镜像失败")
	}
	if written > limit {
		_ = root.Remove(name)
		return fmt.Errorf("镜像超过大小限制")
	}
	return nil
}

func fetchLibraryURL(dir, name, rawURL, checksum string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || !validLibraryName(name) || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || strings.Contains(parsed.Path, "..") {
		return fmt.Errorf("只允许通过 HTTPS 地址拉取镜像")
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(parsed.String()) // #nosec G704 -- scheme, host, credentials, and path are checked above and redirects are disabled.
	if err != nil || response.StatusCode != http.StatusOK {
		return fmt.Errorf("无法拉取镜像")
	}
	defer response.Body.Close()
	if err := saveLibraryUpload(dir, name, response.Body, 512<<20); err != nil {
		return err
	}
	if checksum != "" {
		sum, err := librarySHA256(dir, name)
		if err != nil || !strings.EqualFold(sum, checksum) {
			root, rootErr := os.OpenRoot(dir)
			if rootErr == nil {
				_ = root.Remove(name)
				_ = root.Close()
			}
			return fmt.Errorf("SHA256 校验失败")
		}
	}
	return nil
}

func libraryChecksum(dir, name string) (string, error) {
	if !validLibraryName(name) {
		return "", fmt.Errorf("镜像名称无效")
	}
	path := filepath.Join(dir, name)
	if !validInstallISO(dir, path) && !validLibraryDisk(dir, path) {
		return "", fmt.Errorf("镜像不在镜像库中")
	}
	return librarySHA256(dir, name)
}
