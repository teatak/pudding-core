package tool

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestInheritedGitIgnoreFilesAreExactAndCannotBeRedirected(t *testing.T) {
	for _, useXDG := range []bool{false, true} {
		userHome, config := t.TempDir(), t.TempDir()
		env := []string{"HOME=" + userHome}
		path := filepath.Join(userHome, ".config", "git", "ignore")
		if useXDG {
			env = append(env, "XDG_CONFIG_HOME="+config)
			path = filepath.Join(config, "git", "ignore")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("*.ignored\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		canonicalPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := inheritedGitIgnoreFiles(env, env); !slices.Equal(got, []sandboxReadFile{{path: canonicalPath, lookup: path}}) {
			t.Fatalf("default ignore not exactly granted: %v", got)
		}
		redirected := []string{"HOME=" + userHome, "XDG_CONFIG_HOME=" + t.TempDir()}
		if got := inheritedGitIgnoreFiles(env, redirected); len(got) != 0 {
			t.Fatalf("custom env redirected file grant: %v", got)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "actual-ignore")
		if err := os.WriteFile(outside, []byte("*.ignored\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		got := inheritedGitIgnoreFiles(env, env)
		canonicalOutside, err := filepath.EvalSymlinks(outside)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, []sandboxReadFile{{path: canonicalOutside, lookup: path}}) {
			t.Fatalf("symlink grant is not limited to exact file paths: %v", got)
		}
		if err := os.Remove(outside); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(outside, 0o700); err != nil {
			t.Fatal(err)
		}
		if got := inheritedGitIgnoreFiles(env, env); len(got) != 0 {
			t.Fatalf("ignore directory was granted: %v", got)
		}
	}
}
