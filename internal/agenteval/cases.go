// Package agenteval runs end-to-end coding-agent evaluations against isolated fixtures.
package agenteval

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultCaseTimeout = 10 * time.Minute

type Case struct {
	Name        string
	Description string
	Prompt      string
	FixtureDir  string
	Verify      VerifySpec
	SourceFile  string
}

type VerifySpec struct {
	Command          []string
	Timeout          time.Duration
	BaselineMustFail bool
	AllowedPaths     []string
}

type caseYAML struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Prompt      string     `yaml:"prompt"`
	Fixture     string     `yaml:"fixture"`
	Verify      verifyYAML `yaml:"verify"`
}

type verifyYAML struct {
	Command          []string `yaml:"command"`
	Timeout          string   `yaml:"timeout"`
	BaselineMustFail bool     `yaml:"baseline_must_fail"`
	AllowedPaths     []string `yaml:"allowed_paths"`
}

func LoadCases(dir string, selected []string) ([]Case, error) {
	dir = filepath.Clean(strings.TrimSpace(dir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("agent eval: read cases directory: %w", err)
	}
	wanted := make(map[string]bool, len(selected))
	for _, name := range selected {
		name = strings.TrimSpace(name)
		if name != "" {
			wanted[name] = true
		}
	}
	seen := make(map[string]bool)
	var out []Case
	for _, entry := range entries {
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yaml" && filepath.Ext(entry.Name()) != ".yml") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		item, err := loadCase(path)
		if err != nil {
			return nil, err
		}
		if seen[item.Name] {
			return nil, fmt.Errorf("agent eval: duplicate case name %q", item.Name)
		}
		seen[item.Name] = true
		if len(wanted) == 0 || wanted[item.Name] {
			out = append(out, item)
		}
	}
	if len(wanted) > 0 {
		for name := range wanted {
			if !seen[name] {
				return nil, fmt.Errorf("agent eval: case %q not found", name)
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("agent eval: no cases selected")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func loadCase(path string) (Case, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Case{}, fmt.Errorf("agent eval: read case %s: %w", path, err)
	}
	var in caseYAML
	if err := yaml.Unmarshal(raw, &in); err != nil {
		return Case{}, fmt.Errorf("agent eval: decode case %s: %w", path, err)
	}
	timeout := defaultCaseTimeout
	if strings.TrimSpace(in.Verify.Timeout) != "" {
		timeout, err = time.ParseDuration(strings.TrimSpace(in.Verify.Timeout))
		if err != nil || timeout <= 0 {
			return Case{}, fmt.Errorf("agent eval: case %s has invalid verify timeout %q", path, in.Verify.Timeout)
		}
	}
	item := Case{
		Name:        strings.TrimSpace(in.Name),
		Description: strings.TrimSpace(in.Description),
		Prompt:      strings.TrimSpace(in.Prompt),
		FixtureDir:  filepath.Clean(filepath.Join(filepath.Dir(path), in.Fixture)),
		SourceFile:  path,
		Verify: VerifySpec{
			Command:          append([]string(nil), in.Verify.Command...),
			Timeout:          timeout,
			BaselineMustFail: in.Verify.BaselineMustFail,
			AllowedPaths:     normalizePaths(in.Verify.AllowedPaths),
		},
	}
	if item.Name == "" || item.Prompt == "" || strings.TrimSpace(in.Fixture) == "" || len(item.Verify.Command) == 0 {
		return Case{}, fmt.Errorf("agent eval: case %s requires name, prompt, fixture, and verify.command", path)
	}
	if len(item.Verify.AllowedPaths) == 0 {
		return Case{}, fmt.Errorf("agent eval: case %s requires at least one safe verify.allowed_paths entry", path)
	}
	info, err := os.Stat(item.FixtureDir)
	if err != nil || !info.IsDir() {
		return Case{}, fmt.Errorf("agent eval: case %s fixture %s is not a directory", path, item.FixtureDir)
	}
	return item, nil
}

func normalizePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
		if path == "" || path == "." || strings.HasPrefix(path, "../") || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
