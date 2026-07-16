package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrInvalidAsset = errors.New("app: invalid asset")

func ReadAsset(root, rel string) ([]byte, string, error) {
	resolvedRoot, err := resolveAppRoot(root, false)
	if err != nil {
		return nil, "", ErrInvalidAsset
	}
	root = resolvedRoot
	rel = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(rel)), "/")
	cleaned, err := cleanRelativeSlashPath(rel)
	if err != nil {
		return nil, "", ErrInvalidAsset
	}
	parts := strings.Split(cleaned, "/")
	if len(parts) != 3 || !appIDPattern.MatchString(parts[0]) || parts[1] != "assets" {
		return nil, "", ErrInvalidAsset
	}
	contentType, ok := iconContentType(parts[2])
	if !ok {
		return nil, "", ErrInvalidAsset
	}
	target, err := resolveAppRegularFile(filepath.Join(root, parts[0]), filepath.ToSlash(filepath.Join("assets", parts[2])))
	if err != nil {
		return nil, "", ErrInvalidAsset
	}
	data, err := os.ReadFile(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", ErrInvalidAsset
		}
		return nil, "", err
	}
	return data, contentType, nil
}

func iconContentType(name string) (string, bool) {
	switch strings.ToLower(name) {
	case "icon.png":
		return "image/png", true
	case "icon.jpg", "icon.jpeg":
		return "image/jpeg", true
	case "icon.svg":
		return "image/svg+xml", true
	case "icon.webp":
		return "image/webp", true
	default:
		return "", false
	}
}
