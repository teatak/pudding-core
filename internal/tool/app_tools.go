package tool

import "github.com/teatak/pudding-core/internal/app"

var builtinAppTools = map[string]string{
	BrowserStatus:     app.BuiltinBrowserID,
	BrowserOpen:       app.BuiltinBrowserID,
	BrowserObserve:    app.BuiltinBrowserID,
	BrowserScreenshot: app.BuiltinBrowserID,
	BrowserBack:       app.BuiltinBrowserID,
	BrowserForward:    app.BuiltinBrowserID,
	BrowserReload:     app.BuiltinBrowserID,
	BrowserClose:      app.BuiltinBrowserID,
	BrowserClick:      app.BuiltinBrowserID,
	BrowserType:       app.BuiltinBrowserID,
	BrowserScroll:     app.BuiltinBrowserID,
	SkillValidate:     app.BuiltinSkillAuthoringID,
	AppSave:           app.BuiltinAppAuthoringID,
	FileList:          app.BuiltinProjectFilesID,
	FileRead:          app.BuiltinProjectFilesID,
	FileStat:          app.BuiltinProjectFilesID,
	FileSearch:        app.BuiltinProjectFilesID,
	FileSlice:         app.BuiltinProjectFilesID,
	FileWrite:         app.BuiltinProjectFilesID,
	FilePatch:         app.BuiltinProjectFilesID,
	FileDelete:        app.BuiltinProjectFilesID,
	FileMove:          app.BuiltinProjectFilesID,
	FileCopy:          app.BuiltinProjectFilesID,
	GitStatus:         app.BuiltinSourceControlID,
	GitDiff:           app.BuiltinSourceControlID,
	GitLog:            app.BuiltinSourceControlID,
	GitStage:          app.BuiltinSourceControlID,
	GitUnstage:        app.BuiltinSourceControlID,
	GitCommit:         app.BuiltinSourceControlID,
	CodeSymbols:       app.BuiltinCodeIntelID,
	CodeDefinition:    app.BuiltinCodeIntelID,
	CodeReferences:    app.BuiltinCodeIntelID,
	CodeDiagnostics:   app.BuiltinCodeIntelID,
	CodeRename:        app.BuiltinCodeIntelID,
	CameraCapture:     app.BuiltinCaptureID,
	DesktopScreenshot: app.BuiltinCaptureID,
}

func BuiltinAppIDForTool(name string) (string, bool) {
	id, ok := builtinAppTools[name]
	return id, ok
}

func IsAppAPITool(name string) bool {
	switch name {
	case RESTRequest, GraphQLRequest, GraphQLIntrospect, GraphQLSearch:
		return true
	default:
		return false
	}
}
