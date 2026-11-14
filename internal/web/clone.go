package web

import "webvirtcloud-go/internal/libvirt"

func cloneArgs(source, target string, linked bool) ([]string, error) {
	if err := libvirt.ValidateName(source); err != nil || libvirt.ValidateName(target) != nil || source == target {
		return nil, errOr(err, "克隆名称无效")
	}
	args := []string{"--original", source, "--name", target, "--auto-clone"}
	if linked {
		args = append(args, "--reflink")
	}
	return args, nil
}

func errOr(err error, fallback string) error {
	if err != nil {
		return err
	}
	return errorString(fallback)
}

type errorString string

func (e errorString) Error() string { return string(e) }
