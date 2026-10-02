package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/teatak/pudding-core/internal/plugin"
)

const (
	maxAuthoredPluginFiles     = 64
	maxAuthoredPluginFileBytes = 256 * 1024
	maxAuthoredPluginTotal     = 1024 * 1024
)

type pluginSaveRequest struct {
	Operation string                  `json:"operation"`
	PluginID  string                  `json:"plugin_id"`
	Version   string                  `json:"version"`
	Files     []pluginSaveRequestFile `json:"files"`
}

type pluginSaveRequestFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (r *BuiltinRunner) pluginSave(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	request, err := decodePluginSaveRequest(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if r.pluginAuthoring == nil {
		return toolJSONError(out, "plugin_authoring_unavailable", "plugin authoring is not configured")
	}

	definitions, err := r.pluginAuthoring.ListDefinitions(ctx)
	if err != nil {
		return toolJSONError(out, "plugin_lookup_failed", err.Error())
	}
	existing := findPluginDefinition(definitions, request.PluginID)
	if existing != nil && existing.Source != plugin.SourceInstalled {
		return toolJSONError(out, "plugin_not_editable", "built-in and runtime plugins cannot be created or updated")
	}
	switch request.Operation {
	case "create":
		if existing != nil {
			return toolJSONError(out, "plugin_exists", "a plugin with this id is already installed")
		}
	case "update":
		if existing == nil {
			return toolJSONError(out, "plugin_not_found", "the installed plugin does not exist")
		}
	}

	files := make([]plugin.PackageFile, 0, len(request.Files))
	for _, file := range request.Files {
		files = append(files, plugin.PackageFile{Path: file.Path, Content: file.Content})
	}
	pkg := plugin.Package{
		Kind:          plugin.PluginPackageKind,
		SchemaVersion: plugin.PluginPackageSchemaVersion,
		Plugin:        plugin.PackagePlugin{ID: request.PluginID, Version: request.Version},
		Files:         files,
	}
	packageJSON, err := json.Marshal(pkg)
	if err != nil {
		return toolJSONError(out, "plugin_package_failed", err.Error())
	}
	definition, err := r.pluginAuthoring.SaveAuthoredPackage(ctx, packageJSON, request.Operation == "update")
	if err != nil {
		reason := "plugin_save_failed"
		switch {
		case errors.Is(err, plugin.ErrBuiltinPlugin):
			reason = "plugin_not_editable"
		case errors.Is(err, plugin.ErrAlreadyExists):
			reason = "plugin_exists"
		case errors.Is(err, plugin.ErrNotFound):
			reason = "plugin_not_found"
		}
		return toolJSONError(out, reason, err.Error())
	}

	operation := "created"
	if request.Operation == "update" {
		operation = "updated"
	}
	payload := map[string]any{
		"ok":                 true,
		"operation":          operation,
		"pluginID":           definition.ID,
		"name":               definition.Name,
		"version":            definition.Version,
		"enabled":            definition.Enabled,
		"files":              len(request.Files),
		"skills":             len(definition.Skills),
		"tools":              len(definition.Tools),
		"connectionRequired": pluginConnectionRequired(definition),
	}
	out.Ok = true
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

func decodePluginSaveRequest(raw json.RawMessage) (pluginSaveRequest, error) {
	var request pluginSaveRequest
	if len(raw) == 0 {
		return request, errors.New("arguments are required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return request, err
	}
	request.Operation = strings.ToLower(strings.TrimSpace(request.Operation))
	if request.Operation != "create" && request.Operation != "update" {
		return request, errors.New("operation must be create or update")
	}
	request.PluginID = strings.TrimSpace(request.PluginID)
	if request.PluginID == "" {
		return request, errors.New("plugin_id is required")
	}
	request.Version = strings.TrimSpace(request.Version)
	if request.Version == "" {
		return request, errors.New("version is required")
	}
	if len(request.Files) == 0 || len(request.Files) > maxAuthoredPluginFiles {
		return request, fmt.Errorf("files must contain between 1 and %d items", maxAuthoredPluginFiles)
	}
	total := 0
	for index := range request.Files {
		request.Files[index].Path = strings.TrimSpace(request.Files[index].Path)
		if request.Files[index].Path == "" {
			return request, fmt.Errorf("files[%d].path is required", index)
		}
		size := len([]byte(request.Files[index].Content))
		if size > maxAuthoredPluginFileBytes {
			return request, fmt.Errorf("files[%d] exceeds %d bytes", index, maxAuthoredPluginFileBytes)
		}
		total += size
		if total > maxAuthoredPluginTotal {
			return request, fmt.Errorf("plugin package exceeds %d bytes", maxAuthoredPluginTotal)
		}
	}
	return request, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("arguments must contain one JSON object")
}

func findPluginDefinition(definitions []*plugin.Definition, id string) *plugin.Definition {
	for _, definition := range definitions {
		if definition != nil && definition.ID == id {
			return definition
		}
	}
	return nil
}

func pluginConnectionRequired(definition *plugin.Definition) bool {
	if definition == nil {
		return false
	}
	if definition.Auth != nil && definition.Auth.Required {
		return true
	}
	if definition.Connection != nil {
		for _, field := range definition.Connection.Fields {
			if field.Required {
				return true
			}
		}
	}
	return false
}
