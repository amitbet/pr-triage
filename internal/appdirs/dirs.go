package appdirs

import (
	"os"
	"path/filepath"
)

func CacheDir() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "pr-manager")
	// The project was called pr-triage. Move its cache, drafts included,
	// the first time the new name is used, and leave a symlink so a
	// pr-triage build that is still running keeps finding its files.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		old := filepath.Join(root, "pr-triage")
		if os.Rename(old, dir) == nil {
			_ = os.Symlink("pr-manager", old)
		}
	}
	return dir, nil
}
