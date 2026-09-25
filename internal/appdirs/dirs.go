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
	return filepath.Join(root, "pr-triage"), nil
}
