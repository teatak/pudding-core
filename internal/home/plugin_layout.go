package home

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Before plugins were renamed from Apps (schema v27), packages lived in
// <home>/apps as app.yaml with an .pudding-app-lock.json, connections in
// config/app-connections.yaml, and enablement under settings.yaml "apps".
// Prepare moves that layout once. Every step is idempotent, so an interrupted
// upgrade completes on the next start.
func migratePluginLayout(dir string) error {
	if err := movePluginPackages(dir); err != nil {
		return err
	}
	if err := movePluginConnections(dir); err != nil {
		return err
	}
	return renameSettingsPlugins(dir)
}

// Built-in plugin IDs renamed with the concept.
var renamedPluginIDs = map[string]string{"app-authoring": "plugin-authoring", "canvas": "widget-authoring"}

func movePluginPackages(dir string) error {
	old := filepath.Join(dir, "apps")
	entries, err := os.ReadDir(old)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("home: read apps: %w", err)
	}
	target := PluginsPath(dir)
	if err := os.MkdirAll(target, 0o700); err != nil {
		return fmt.Errorf("home: mkdir %s: %w", target, err)
	}
	kept := 0
	for _, entry := range entries {
		from, to := filepath.Join(old, entry.Name()), filepath.Join(target, entry.Name())
		if _, err := os.Lstat(to); err == nil {
			slog.Warn("home: plugin exists in both apps and plugins; keeping both", "name", entry.Name())
			kept++
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			if err := renamePackageFiles(from); err != nil {
				return fmt.Errorf("home: convert plugin %s: %w", entry.Name(), err)
			}
		}
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("home: move plugin %s: %w", entry.Name(), err)
		}
	}
	if kept > 0 {
		return nil
	}
	return os.Remove(old)
}

func renamePackageFiles(pkg string) error {
	err := convertFile(filepath.Join(pkg, "app.yaml"), filepath.Join(pkg, "plugin.yaml"), func(data []byte) ([]byte, error) {
		return editYAML(data, func(root *yaml.Node) bool {
			if kind := mappingValue(root, "kind"); kind != nil && kind.Kind == yaml.ScalarNode && kind.Value == "app" {
				kind.Value = "plugin"
				return true
			}
			return false
		})
	})
	if err != nil {
		return err
	}
	return convertFile(filepath.Join(pkg, ".pudding-app-lock.json"), filepath.Join(pkg, ".pudding-plugin-lock.json"), func(data []byte) ([]byte, error) {
		return bytes.Replace(data, []byte(`"pudding.app.lock"`), []byte(`"pudding.plugin.lock"`), 1), nil
	})
}

func movePluginConnections(dir string) error {
	config := filepath.Join(dir, "config")
	return convertFile(filepath.Join(config, "app-connections.yaml"), filepath.Join(config, "plugin-connections.yaml"), func(data []byte) ([]byte, error) {
		return editYAML(data, func(root *yaml.Node) bool {
			connections := mappingValue(root, "connections")
			if connections == nil || connections.Kind != yaml.MappingNode {
				return false
			}
			changed := false
			for i := 1; i < len(connections.Content); i += 2 {
				if key := mappingKey(connections.Content[i], "app"); key != nil {
					key.Value, changed = "plugin", true
				}
			}
			return changed
		})
	})
}

func renameSettingsPlugins(dir string) error {
	path := filepath.Join(dir, "config", "settings.yaml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	out, err := editYAML(data, func(root *yaml.Node) bool {
		key := mappingKey(root, "apps")
		if key == nil || mappingKey(root, "plugins") != nil {
			return false
		}
		key.Value = "plugins"
		if enabled := mappingValue(mappingValue(root, "plugins"), "enabled"); enabled != nil && enabled.Kind == yaml.MappingNode {
			for i := 0; i < len(enabled.Content); i += 2 {
				if renamed, ok := renamedPluginIDs[enabled.Content[i].Value]; ok && mappingKey(enabled, renamed) == nil {
					enabled.Content[i].Value = renamed
				}
			}
		}
		return true
	})
	if err != nil || bytes.Equal(out, data) {
		return err
	}
	return writeFileAtomic(path, out)
}

// convertFile writes the converted content under the new name, then removes the
// old file. A new file left by an interrupted attempt is already complete.
func convertFile(oldPath, newPath string, convert func([]byte) ([]byte, error)) error {
	data, err := os.ReadFile(oldPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := os.Stat(newPath); err == nil {
		return os.Remove(oldPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out, err := convert(data)
	if err != nil {
		return fmt.Errorf("home: convert %s: %w", oldPath, err)
	}
	if err := writeFileAtomic(newPath, out); err != nil {
		return err
	}
	return os.Remove(oldPath)
}

// editYAML rewrites a document only when edit reports a change, so files that
// need nothing keep their exact bytes.
func editYAML(data []byte, edit func(root *yaml.Node) bool) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode || !edit(doc.Content[0]) {
		return data, nil
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(4)
	if err := encoder.Encode(&doc); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func mappingKey(node *yaml.Node, name string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return node.Content[i]
		}
	}
	return nil
}

func mappingValue(node *yaml.Node, name string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return node.Content[i+1]
		}
	}
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
