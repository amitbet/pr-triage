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
	// the first time the new name is used.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		_ = os.Rename(filepath.Join(root, "pr-triage"), dir)
	}
	return dir, nil
}
