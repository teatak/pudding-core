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
	CommandStart:      app.BuiltinTerminalID,
	CommandPoll:       app.BuiltinTerminalID,
	CommandStop:       app.BuiltinTerminalID,
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
