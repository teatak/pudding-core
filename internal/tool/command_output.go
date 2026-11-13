package tool

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"sync"

	"github.com/teatak/pudding-core/internal/home"
)

const (
	commandOutputFileLimit    = 8 << 20
	commandOutputSessionLimit = 64 << 20
)

// Foreground calls are serialized by the engine's session-artifacts resource.
// The mutex here protects the shared quota from stdout/stderr copy goroutines.
type commandOutputCapture struct {
	mu                 sync.Mutex
	homeDir, sessionID string
	root               *os.Root
	path               string
	remaining          int64
	initialized        bool
	err                error
}

type commandOutputFile struct {
	Scope      string `json:"scope,omitempty"`
	Path       string `json:"path,omitempty"`
	Bytes      int64  `json:"bytes"`
	TotalBytes int64  `json:"totalBytes"`
	Complete   bool   `json:"complete"`
	Reason     string `json:"reason,omitempty"`
}

type commandOutputArtifact struct {
	capture *commandOutputCapture
	stream  string
	file    *os.File
	started bool
	info    commandOutputFile
}

func (c *commandOutputCapture) open() error {
	if c.initialized {
		return c.err
	}
	c.initialized = true
	c.root, c.path, c.err = home.OpenSessionCommandOutputs(c.homeDir, c.sessionID)
	if c.err != nil {
		return c.err
	}
	dir, err := c.root.Open(".")
	if err != nil {
		c.err = err
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		c.err = err
		return err
	}
	c.remaining = commandOutputSessionLimit
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			c.err = err
			return err
		}
		if info.Mode().IsRegular() {
			c.remaining -= info.Size()
		}
	}
	return nil
}

func (c *commandOutputCapture) close() {
	if c.root != nil {
		c.root.Close()
	}
}

func (a *commandOutputArtifact) write(head, tail, data []byte) {
	c := a.capture
	c.mu.Lock()
	defer c.mu.Unlock()
	if a.info.Reason != "" {
		return
	}
	if !a.started {
		a.started = true
		if err := c.open(); err != nil {
			a.info.Reason = "storage_unavailable"
			return
		}
		// Do not leave empty files when the session quota has been exhausted.
		if c.remaining <= 0 {
			a.info.Reason = "session_limit"
			return
		}
		name := a.stream + "-" + rand.Text() + ".txt"
		file, err := c.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			a.info.Reason = "storage_unavailable"
			return
		}
		a.file = file
		a.info.Scope = managedScopeTemp
		a.info.Path = filepath.Join("session-artifacts", filepath.Base(filepath.Dir(c.path)), "command-output", name)
		a.append(head)
		a.append(tail)
	}
	a.append(data)
}

// Storage failure never interrupts the command's output pipe. The result
// reports incomplete capture separately from the process exit status.
func (a *commandOutputArtifact) append(data []byte) {
	if a.info.Reason != "" || len(data) == 0 {
		return
	}
	available := min(int64(commandOutputFileLimit)-a.info.Bytes, a.capture.remaining)
	count := min(int64(len(data)), available)
	if count > 0 {
		n, err := a.file.Write(data[:count])
		a.info.Bytes += int64(n)
		a.capture.remaining -= int64(n)
		if err != nil || int64(n) != count {
			a.info.Reason = "write_failed"
			return
		}
	}
	if count < int64(len(data)) {
		a.info.Reason = "stream_limit"
		if a.capture.remaining <= 0 {
			a.info.Reason = "session_limit"
		}
	}
}

func (a *commandOutputArtifact) finish(total int) commandOutputFile {
	if a.file != nil {
		if err := a.file.Close(); err != nil && a.info.Reason == "" {
			a.info.Reason = "write_failed"
		}
		a.file = nil
	}
	a.info.TotalBytes = int64(total)
	a.info.Complete = a.info.Reason == "" && a.info.Bytes == int64(total)
	return a.info
}
