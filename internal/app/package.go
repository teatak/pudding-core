package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	AppPackageKind          = "pudding.app.package"
	AppPackageSchemaVersion = 1
	AppLockFileName         = ".pudding-app-lock.json"
	AppLockKind             = "pudding.app.lock"
)

type Package struct {
	Kind          string        `json:"kind"`
	SchemaVersion int           `json:"schema_version"`
	App           PackageApp    `json:"app"`
	Files         []PackageFile `json:"files"`
}

type PackageApp struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type PackageFile struct {
	Path          string `json:"path"`
	Content       string `json:"content,omitempty"`
	ContentBase64 string `json:"content_base64,omitempty"`
}

type PackageLock struct {
	Kind          string            `json:"kind"`
	SchemaVersion int               `json:"schema_version"`
	SourceURL     string            `json:"source_url,omitempty"`
	Version       string            `json:"version,omitempty"`
	PackageSHA256 string            `json:"package_sha256,omitempty"`
	Files         []PackageLockFile `json:"files"`
}

type PackageLockFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func InstallPackage(root string, packageJSON []byte, expectedSHA256, sourceURL string) (*Definition, error) {
	if len(packageJSON) == 0 {
		return nil, errors.New("app package is required")
	}
	actualSHA := sha256Bytes(packageJSON)
	expectedSHA256 = strings.ToLower(strings.TrimSpace(expectedSHA256))
	if expectedSHA256 != "" && actualSHA != expectedSHA256 {
		return nil, fmt.Errorf("app package sha256 mismatch")
	}

	var pkg Package
	if err := json.Unmarshal(packageJSON, &pkg); err != nil {
		return nil, fmt.Errorf("app package: parse: %w", err)
	}
	if pkg.Kind != AppPackageKind {
		return nil, fmt.Errorf("unsupported app package kind %q", pkg.Kind)
	}
	if pkg.SchemaVersion != AppPackageSchemaVersion {
		return nil, fmt.Errorf("unsupported app package schema %d", pkg.SchemaVersion)
	}
	appID := strings.TrimSpace(pkg.App.ID)
	if !appIDPattern.MatchString(appID) {
		return nil, fmt.Errorf("invalid app id %q", pkg.App.ID)
	}
	appVersion := strings.TrimSpace(pkg.App.Version)
	if appVersion == "" {
		return nil, errors.New("app package version is required")
	}
	files, err := packageFiles(pkg.Files)
	if err != nil {
		return nil, err
	}
	if _, ok := files[AppFileName]; !ok {
		return nil, fmt.Errorf("%s is required", AppFileName)
	}

	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp(root, ".app-install-"+appID+"-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	if err := writePackageFiles(tempDir, files); err != nil {
		return nil, err
	}
	tempDef, err := LoadDefinitionDir(tempDir)
	if err != nil {
		return nil, err
	}
	if tempDef.ID != appID {
		return nil, fmt.Errorf("package app id %q does not match %s id %q", appID, AppFileName, tempDef.ID)
	}
	if tempDef.Version != "" && tempDef.Version != appVersion {
		return nil, fmt.Errorf("package version %q does not match %s version %q", appVersion, AppFileName, tempDef.Version)
	}

	dest := filepath.Join(root, appID)
	oldLock, _ := readPackageLock(dest)
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return nil, err
	}
	if err := writePackageFiles(dest, files); err != nil {
		return nil, err
	}
	if err := removeOldPackageFiles(dest, oldLock, files); err != nil {
		return nil, err
	}
	lock := buildPackageLock(files, appVersion, actualSHA, sourceURL)
	if err := writePackageLock(dest, lock); err != nil {
		return nil, err
	}
	return LoadDefinitionDir(dest)
}

func packageFiles(in []PackageFile) (map[string][]byte, error) {
	if len(in) == 0 {
		return nil, errors.New("app package files are required")
	}
	out := make(map[string][]byte, len(in))
	for _, file := range in {
		cleaned, err := cleanRelativeSlashPath(file.Path)
		if err != nil {
			return nil, err
		}
		if cleaned == AppLockFileName || cleaned == MCPOverrideFileName {
			return nil, fmt.Errorf("%s is reserved", cleaned)
		}
		if _, exists := out[cleaned]; exists {
			return nil, fmt.Errorf("duplicate package file %q", cleaned)
		}
		data := []byte(file.Content)
		if file.ContentBase64 != "" {
			data, err = base64.StdEncoding.DecodeString(file.ContentBase64)
			if err != nil {
				return nil, fmt.Errorf("decode %s: %w", cleaned, err)
			}
		}
		out[cleaned] = data
	}
	return out, nil
}

func writePackageFiles(root string, files map[string][]byte) error {
	for _, file := range sortedPackagePaths(files) {
		target := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, files[file], 0o600); err != nil {
			return err
		}
	}
	return nil
}

func removeOldPackageFiles(root string, oldLock *PackageLock, current map[string][]byte) error {
	if oldLock == nil {
		return nil
	}
	for _, file := range oldLock.Files {
		cleaned, err := cleanRelativeSlashPath(file.Path)
		if err != nil {
			continue
		}
		if _, keep := current[cleaned]; keep {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(cleaned))
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func buildPackageLock(files map[string][]byte, version, packageSHA256, sourceURL string) PackageLock {
	lock := PackageLock{
		Kind:          AppLockKind,
		SchemaVersion: AppPackageSchemaVersion,
		SourceURL:     strings.TrimSpace(sourceURL),
		Version:       strings.TrimSpace(version),
		PackageSHA256: strings.TrimSpace(packageSHA256),
		Files:         make([]PackageLockFile, 0, len(files)),
	}
	for _, file := range sortedPackagePaths(files) {
		lock.Files = append(lock.Files, PackageLockFile{
			Path:   file,
			SHA256: sha256Bytes(files[file]),
		})
	}
	return lock
}

func readPackageLock(dir string) (*PackageLock, error) {
	data, err := os.ReadFile(filepath.Join(dir, AppLockFileName))
	if err != nil {
		return nil, err
	}
	var lock PackageLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	if lock.Kind != AppLockKind {
		return nil, fmt.Errorf("unsupported app lock kind %q", lock.Kind)
	}
	return &lock, nil
}

func writePackageLock(dir string, lock PackageLock) error {
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(dir, AppLockFileName), data, 0o600)
}

func applyDefinitionLock(dir string, def *Definition) {
	lock, err := readPackageLock(dir)
	if err != nil || lock == nil || def == nil {
		return
	}
	def.SourceURL = lock.SourceURL
	def.PackageSHA256 = lock.PackageSHA256
	if def.Version == "" {
		def.Version = lock.Version
	}
}

func sortedPackagePaths(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for file := range files {
		out = append(out, file)
	}
	sort.Strings(out)
	return out
}

func sha256Bytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
