package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/tool"
)

func TestWorkspaceWriteOverlap(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "pkg")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	aliasErr := os.Symlink(root, alias)
	if aliasErr != nil && runtime.GOOS != "windows" {
		t.Fatal(aliasErr)
	}
	for _, tc := range []struct {
		name, first, second string
		overlap             bool
	}{
		{"same", root, root, true},
		{"parent", root, child, true},
		{"child", child, root, true},
		{"alias", root, alias, true},
		{"alias_child", child, filepath.Join(alias, "pkg"), true},
		{"missing_alias_child", filepath.Join(root, "missing"), filepath.Join(alias, "missing", "nested"), true},
		{"siblings", child, filepath.Join(root, "other"), false},
		{"prefix_only", child, child + "-other", false},
		{"filesystem_root", filepath.VolumeName(root) + string(filepath.Separator), root, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Contains(tc.name, "alias") && aliasErr != nil {
				t.Skipf("symlink creation unavailable: %v", aliasErr)
			}
			eng := &Engine{}
			release, err := eng.AcquireWorkspaceWrite(context.Background(), []string{tc.first})
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			second, err := eng.AcquireWorkspaceWrite(ctx, []string{tc.second})
			if tc.overlap {
				if !errors.Is(err, context.DeadlineExceeded) {
					if second != nil {
						second()
					}
					t.Fatalf("overlap acquired: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				second()
			}
			release()
			release() // Release is idempotent; canceled waiters leave no lease behind.
			nextCtx, nextCancel := context.WithTimeout(context.Background(), time.Second)
			defer nextCancel()
			next, err := eng.AcquireWorkspaceWrite(nextCtx, []string{tc.second})
			if err != nil {
				t.Fatal(err)
			}
			next()
		})
	}
}

func TestWorkspaceWriteMultipleRootsAndCancellation(t *testing.T) {
	eng := &Engine{}
	left, right := t.TempDir(), t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var workers sync.WaitGroup
	for i := 0; i < 10; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			roots := []string{left, right}
			if i%2 == 0 {
				roots = []string{right, left, right}
			}
			release, err := eng.AcquireWorkspaceWrite(ctx, roots)
			if err != nil {
				t.Error(err)
				return
			}
			release()
		}(i)
	}
	workers.Wait()
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if release, err := eng.AcquireWorkspaceWrite(canceled, []string{left}); !errors.Is(err, context.Canceled) {
		if release != nil {
			release()
		}
		t.Fatalf("canceled acquire: %v", err)
	}
}

func TestToolResourcesShareWorkspaceWrites(t *testing.T) {
	eng := &Engine{}
	root := t.TempDir()
	call := tool.Call{SessionID: "other-session", Name: tool.FileWrite, Args: json.RawMessage(`{"scope":"project","path":"test.txt","content":"new"}`), ProjectDirs: []string{root}}
	release, err := eng.acquireToolResources(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if next, err := eng.AcquireWorkspaceWrite(ctx, []string{filepath.Join(root, "nested")}); !errors.Is(err, context.DeadlineExceeded) {
		if next != nil {
			next()
		}
		t.Fatalf("API write bypassed tool lease: %v", err)
	}
}
