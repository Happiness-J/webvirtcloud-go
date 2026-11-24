package web

import "strings"

// unusedInstallMedia names installer images that no visible VM has in an
// optical drive. It never deletes a file. A host that cannot be queried is
// reported separately so its missing answer is not treated as unused.
type unusedMedia struct {
	Names      []string
	Incomplete bool
}

func unusedInstallers(images []libraryImage, mounted []cdromMedia, hostsFailed int) unusedMedia {
	used := map[string]bool{}
	for _, item := range mounted {
		if item.Empty || item.Image == "" {
			continue
		}
		used[strings.ToLower(item.Image)] = true
	}
	var names []string
	for _, image := range images {
		if image.Category != "installer" {
			continue
		}
		if used[strings.ToLower(image.Name)] {
			continue
		}
		names = append(names, image.Name)
	}
	return unusedMedia{Names: names, Incomplete: hostsFailed > 0}
}
