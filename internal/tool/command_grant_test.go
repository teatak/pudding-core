package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandSessionGrantBoundaries(t *testing.T) {
	root := t.TempDir()
	chrome := filepath.Join(t.TempDir(), "Google Chrome.app", "Contents", "MacOS", "Google Chrome")
	if err := os.MkdirAll(filepath.Dir(chrome), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chrome, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"page.html", "other.html"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte("<h1>preview</h1>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	base := joinShellCommand([]string{chrome, "--headless=new", "--no-sandbox", "--user-data-dir=.preview-profile", "--screenshot=one.png", "--window-size=800,600", "page.html"})
	makeCall := func(command string) Call {
		raw, _ := json.Marshal(map[string]any{"scope": "project", "execution": "host", "host_access_reason": "headless preview needs host services", "command": command})
		return Call{Name: CommandRun, Args: raw, ProjectDirs: []string{root}}
	}
	baseGrant := CommandSessionGrantForCall(makeCall(base))
	if baseGrant == nil {
		t.Fatal("explicit local headless screenshot should offer bounded session approval")
	}
	writableExecutable := makeCall(base)
	writableExecutable.ProjectDirs = append(writableExecutable.ProjectDirs, filepath.Dir(chrome))
	if grant := CommandSessionGrantForCall(writableExecutable); grant != nil {
		t.Fatal("project-writable executable must not receive a reusable host grant")
	}
	for _, tc := range []struct {
		name, command  string
		eligible, same bool
	}{
		{"same", base, true, true},
		{"output name", strings.Replace(base, "one.png", "two.png", 1), true, true},
		{"viewport", strings.Replace(base, "800,600", "1600,900", 1), true, true},
		{"input", strings.Replace(base, "page.html", "other.html", 1), true, false},
		{"profile", strings.Replace(base, ".preview-profile", ".other-profile", 1), true, false},
		{"flags", strings.Replace(base, "--no-sandbox ", "", 1), true, false},
		{"remote URL", strings.Replace(base, "page.html", "https://example.com", 1), false, false},
		{"external input", strings.Replace(base, "page.html", "/etc/hosts", 1), false, false},
		{"default profile", strings.Replace(base, "--user-data-dir=.preview-profile ", "", 1), false, false},
		{"external profile", strings.Replace(base, ".preview-profile", "/Users/someone/Library/Chrome", 1), false, false},
		{"project as profile", strings.Replace(base, ".preview-profile", root, 1), false, false},
		{"external output", strings.Replace(base, "one.png", "/tmp/one.png", 1), false, false},
		{"non-PNG output", strings.Replace(base, "one.png", "page.html", 1), false, false},
		{"extensions", base + " --load-extension=extension", false, false},
		{"remote debug", base + " --remote-debugging-port=9222", false, false},
		{"unsafe web access", base + " --allow-file-access-from-files", false, false},
		{"duplicate output", base + " --screenshot=two.png", false, false},
		{"compound", base + " && echo done", false, false},
		{"substitution", base + " $(echo --hide-scrollbars)", false, false},
		{"expanded temp", strings.Replace(base, ".preview-profile", "$TMPDIR/profile", 1), false, false},
		{"redirect", base + " > log.txt", false, false},
		{"shell wrapper", "sh -c " + quoteShellArg(base), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grant := CommandSessionGrantForCall(makeCall(tc.command))
			if (grant != nil) != tc.eligible {
				t.Fatalf("grant=%+v eligible=%v", grant, tc.eligible)
			}
			if grant != nil && (grant.Key == baseGrant.Key) != tc.same {
				t.Fatalf("incorrect grant reuse: %+v", grant)
			}
		})
	}
	for _, extra := range []map[string]any{{"execution": "sandbox", "host_access_reason": ""}, {"background": true}, {"env": map[string]string{"TEST": "1"}}} {
		call := makeCall(base)
		var args map[string]any
		_ = json.Unmarshal(call.Args, &args)
		for k, v := range extra {
			args[k] = v
		}
		call.Args, _ = json.Marshal(args)
		if grant := CommandSessionGrantForCall(call); grant != nil {
			t.Fatalf("unsupported mode/environment got a grant: %s", call.Args)
		}
	}
	// A path changed into an external symlink must not reuse a previous lease.
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".preview-profile")); err != nil {
		t.Fatal(err)
	}
	if grant := CommandSessionGrantForCall(makeCall(base)); grant != nil {
		t.Fatal("profile symlink escaped project")
	}
	if err := os.Remove(filepath.Join(root, ".preview-profile")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chrome, []byte("#!/bin/sh\necho updated\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if grant := CommandSessionGrantForCall(makeCall(base)); grant == nil || grant.Key == baseGrant.Key {
		t.Fatal("executable update must invalidate the lease")
	}
}
