package daemon

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsDaemonNativeDependenciesAreSQLiteOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-tags", "sqlite_fts5", "-f", "{{if and .CgoFiles (not .Standard)}}{{.ImportPath}}{{end}}", "./cmd/puddingd")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Windows dependency graph: %v\n%s\n%s", err, out, stderr.String())
	}
	if got := strings.TrimSpace(string(out)); got != "github.com/mattn/go-sqlite3" {
		t.Fatalf("unexpected Windows native dependencies: %q", got)
	}
}
