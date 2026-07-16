package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/teatak/pudding-core/internal/app"
)

const (
	maxAuthoredAppFiles     = 64
	maxAuthoredAppFileBytes = 256 * 1024
	maxAuthoredAppTotal     = 1024 * 1024
)

type appSaveRequest struct {
	Operation string               `json:"operation"`
	AppID     string               `json:"app_id"`
	Version   string               `json:"version"`
	Files     []appSaveRequestFile `json:"files"`
}

type appSaveRequestFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (r *BuiltinRunner) appSave(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	request, err := decodeAppSaveRequest(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if r.appAuthoring == nil {
		return toolJSONError(out, "app_authoring_unavailable", "app authoring is not configured")
	}

	definitions, err := r.appAuthoring.ListDefinitions(ctx)
	if err != nil {
		return toolJSONError(out, "app_lookup_failed", err.Error())
	}
	existing := findAppDefinition(definitions, request.AppID)
	if existing != nil && existing.Source != app.SourceInstalled {
		return toolJSONError(out, "app_not_editable", "built-in and runtime Apps cannot be created or updated")
	}
	switch request.Operation {
	case "create":
		if existing != nil {
			return toolJSONError(out, "app_exists", "an App with this id is already installed")
		}
	case "update":
		if existing == nil {
			return toolJSONError(out, "app_not_found", "the installed App does not exist")
		}
	}

	files := make([]app.PackageFile, 0, len(request.Files))
	for _, file := range request.Files {
		files = append(files, app.PackageFile{Path: file.Path, Content: file.Content})
	}
	pkg := app.Package{
		Kind:          app.AppPackageKind,
		SchemaVersion: app.AppPackageSchemaVersion,
		App:           app.PackageApp{ID: request.AppID, Version: request.Version},
		Files:         files,
	}
	packageJSON, err := json.Marshal(pkg)
	if err != nil {
		return toolJSONError(out, "app_package_failed", err.Error())
	}
	definition, err := r.appAuthoring.SaveAuthoredPackage(ctx, packageJSON, request.Operation == "update")
	if err != nil {
		reason := "app_save_failed"
		switch {
		case errors.Is(err, app.ErrBuiltinApp):
			reason = "app_not_editable"
		case errors.Is(err, app.ErrAlreadyExists):
			reason = "app_exists"
		case errors.Is(err, app.ErrNotFound):
			reason = "app_not_found"
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
		"appID":              definition.ID,
		"name":               definition.Name,
		"version":            definition.Version,
		"enabled":            definition.Enabled,
		"files":              len(request.Files),
		"skills":             len(definition.Skills),
		"tools":              len(definition.Tools),
		"connectionRequired": appConnectionRequired(definition),
	}
	out.Ok = true
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

func decodeAppSaveRequest(raw json.RawMessage) (appSaveRequest, error) {
	var request appSaveRequest
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
	request.AppID = strings.TrimSpace(request.AppID)
	if request.AppID == "" {
		return request, errors.New("app_id is required")
	}
	request.Version = strings.TrimSpace(request.Version)
	if request.Version == "" {
		return request, errors.New("version is required")
	}
	if len(request.Files) == 0 || len(request.Files) > maxAuthoredAppFiles {
		return request, fmt.Errorf("files must contain between 1 and %d items", maxAuthoredAppFiles)
	}
	total := 0
	for index := range request.Files {
		request.Files[index].Path = strings.TrimSpace(request.Files[index].Path)
		if request.Files[index].Path == "" {
			return request, fmt.Errorf("files[%d].path is required", index)
		}
		size := len([]byte(request.Files[index].Content))
		if size > maxAuthoredAppFileBytes {
			return request, fmt.Errorf("files[%d] exceeds %d bytes", index, maxAuthoredAppFileBytes)
		}
		total += size
		if total > maxAuthoredAppTotal {
			return request, fmt.Errorf("App package exceeds %d bytes", maxAuthoredAppTotal)
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

func findAppDefinition(definitions []*app.Definition, id string) *app.Definition {
	for _, definition := range definitions {
		if definition != nil && definition.ID == id {
			return definition
		}
	}
	return nil
}

func appConnectionRequired(definition *app.Definition) bool {
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
