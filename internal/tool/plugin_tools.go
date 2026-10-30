package tool

import "github.com/teatak/pudding-core/internal/plugin"

var builtinPluginTools = map[string]string{
	WebSearch: plugin.BuiltinWebSearchFetchID, WebFetch: plugin.BuiltinWebSearchFetchID,
	ArtifactList: plugin.BuiltinArtifactsID, ArtifactOpen: plugin.BuiltinArtifactsID,
	PageCreate: plugin.BuiltinPageAuthoringID, PageRead: plugin.BuiltinPageAuthoringID, PageEdit: plugin.BuiltinPageAuthoringID, PageRename: plugin.BuiltinPageAuthoringID,
	SheetCreate: plugin.BuiltinSheetAuthoringID, SheetRead: plugin.BuiltinSheetAuthoringID, SheetEdit: plugin.BuiltinSheetAuthoringID,
	SubtaskList:       plugin.BuiltinSubsessionID,
	SubtaskDispatch:   plugin.BuiltinSubsessionID,
	SubtaskSend:       plugin.BuiltinSubsessionID,
	SubtaskWait:       plugin.BuiltinSubsessionID,
	SubtaskStop:       plugin.BuiltinSubsessionID,
	SubtaskArchive:    plugin.BuiltinSubsessionID,
	SubtaskRestore:    plugin.BuiltinSubsessionID,
	SubtaskReset:      plugin.BuiltinSubsessionID,
	BrowserStatus:     plugin.BuiltinBrowserID,
	BrowserOpen:       plugin.BuiltinBrowserID,
	BrowserObserve:    plugin.BuiltinBrowserID,
	BrowserScreenshot: plugin.BuiltinBrowserID,
	BrowserBack:       plugin.BuiltinBrowserID,
	BrowserForward:    plugin.BuiltinBrowserID,
	BrowserReload:     plugin.BuiltinBrowserID,
	BrowserClose:      plugin.BuiltinBrowserID,
	BrowserClick:      plugin.BuiltinBrowserID,
	BrowserType:       plugin.BuiltinBrowserID,
	BrowserScroll:     plugin.BuiltinBrowserID,
	SkillValidate:     plugin.BuiltinSkillAuthoringID,
	PluginSave:        plugin.BuiltinPluginAuthoringID,
	CameraCapture:     plugin.BuiltinCaptureID,
	DesktopScreenshot: plugin.BuiltinCaptureID,
	ComputerListApps:  plugin.BuiltinComputerUseID,
	ComputerUseApp:    plugin.BuiltinComputerUseID,
	ComputerQuitApp:   plugin.BuiltinComputerUseID,
	ComputerObserve:   plugin.BuiltinComputerUseID,
	ComputerAct:       plugin.BuiltinComputerUseID,
}

func BuiltinPluginIDForTool(name string) (string, bool) {
	id, ok := builtinPluginTools[NormalizeLegacyContentTool(name)]
	return id, ok
}

func IsPluginAPITool(name string) bool {
	switch name {
	case RESTRequest, GraphQLRequest, GraphQLIntrospect, GraphQLSearch:
		return true
	default:
		return false
	}
}
