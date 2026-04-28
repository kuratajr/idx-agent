package fileserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func NormalizePath(path string) string {
	absPath, err := filepath.Abs(ExpandPath(path))
	if err != nil {
		return path
	}
	return filepath.Clean(absPath)
}

func ExpandPath(path string) string {
	return os.ExpandEnv(strings.TrimSpace(path))
}

func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func IsDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func ValidatePath(root, requested string) (string, error) {
	cleanRoot := filepath.Clean(root)
	cleanRequested := filepath.Clean(requested)
	var fullPath string
	if !filepath.IsAbs(cleanRequested) {
		fullPath = filepath.Join(cleanRoot, cleanRequested)
	} else {
		fullPath = cleanRequested
	}
	absRoot, err := filepath.Abs(cleanRoot)
	if err != nil {
		return "", fmt.Errorf("invalid root path: %v", err)
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("invalid requested path: %v", err)
	}
	relPath, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return "", fmt.Errorf("cannot determine relative path: %v", err)
	}
	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path escapes root directory: %s", requested)
	}
	return absPath, nil
}
