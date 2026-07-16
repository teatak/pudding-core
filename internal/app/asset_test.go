package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAssetAcceptsWildcardPath(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "github", "assets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "icon.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, contentType, err := ReadAsset(root, "/github/assets/icon.svg")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<svg/>" || contentType != "image/svg+xml" {
		t.Fatalf("unexpected asset: %q %s", data, contentType)
	}
}

func TestReadAssetRejectsSymlinkOutsideApp(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "github", "assets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.svg")
	if err := os.WriteFile(outside, []byte("<svg><text>secret</text></svg>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "icon.svg")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, _, err := ReadAsset(root, "/github/assets/icon.svg"); err != ErrInvalidAsset {
		t.Fatalf("asset error = %v, want %v", err, ErrInvalidAsset)
	}
}
