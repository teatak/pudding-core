package tool

import "github.com/teatak/pudding-core/internal/plugin"

var builtinPluginTools = map[string]string{
	StudioList: plugin.BuiltinStudioID, StudioOpen: plugin.BuiltinStudioID, DocCreate: plugin.BuiltinStudioID, DocRead: plugin.BuiltinStudioID, DocEdit: plugin.BuiltinStudioID,
	CollaborationList:     plugin.BuiltinCollaborationID,
	CollaborationDispatch: plugin.BuiltinCollaborationID,
	CollaborationSend:     plugin.BuiltinCollaborationID,
	CollaborationWait:     plugin.BuiltinCollaborationID,
	CollaborationStop:     plugin.BuiltinCollaborationID,
	BrowserStatus:         plugin.BuiltinBrowserID,
	BrowserOpen:           plugin.BuiltinBrowserID,
	BrowserObserve:        plugin.BuiltinBrowserID,
	BrowserScreenshot:     plugin.BuiltinBrowserID,
	BrowserBack:           plugin.BuiltinBrowserID,
	BrowserForward:        plugin.BuiltinBrowserID,
	BrowserReload:         plugin.BuiltinBrowserID,
	BrowserClose:          plugin.BuiltinBrowserID,
	BrowserClick:          plugin.BuiltinBrowserID,
	BrowserType:           plugin.BuiltinBrowserID,
	BrowserScroll:         plugin.BuiltinBrowserID,
	SkillValidate:         plugin.BuiltinSkillAuthoringID,
	PluginSave:            plugin.BuiltinPluginAuthoringID,
	CameraCapture:         plugin.BuiltinCaptureID,
	DesktopScreenshot:     plugin.BuiltinCaptureID,
	ComputerListApps:      plugin.BuiltinComputerUseID,
	ComputerUseApp:        plugin.BuiltinComputerUseID,
	ComputerQuitApp:       plugin.BuiltinComputerUseID,
	ComputerObserve:       plugin.BuiltinComputerUseID,
	ComputerAct:           plugin.BuiltinComputerUseID,
}

func BuiltinPluginIDForTool(name string) (string, bool) {
	id, ok := builtinPluginTools[name]
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
