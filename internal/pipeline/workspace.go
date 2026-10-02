package pipeline

import (
	"fmt"
	"os"
	"path/filepath"

	"slop-generator/internal/config"
)

func outputPath(work, rdir, id string) (string, error) {
	p := filepath.Join(work, rdir, id)
	rel, e := filepath.Rel(work, p)
	if e != nil || !config.SafeRelative(rel) {
		return "", fmt.Errorf("unsafe result path")
	}
	// Existing symlinks must never redirect writes outside the private checkout.
	for cur := p; cur != work; cur = filepath.Dir(cur) {
		info, e := os.Lstat(cur)
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("output path contains a symlink")
		}
		if e != nil && !os.IsNotExist(e) {
			return "", e
		}
	}
	return p, nil
}
