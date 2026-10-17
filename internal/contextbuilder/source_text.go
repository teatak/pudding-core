package contextbuilder

import "strings"

var sourceTextMarkers = strings.NewReplacer(
	"<system-reminder>", "<system-reminder escaped>",
	"</system-reminder>", "</system-reminder escaped>",
	"<message-meta>", "<message-meta escaped>",
	"</message-meta>", "</message-meta escaped>",
)

// EscapeSourceText neutralizes reserved runtime markers in model-only source
// text. Canonical text, tool arguments and opaque provider state stay intact.
func EscapeSourceText(text string) string {
	return sourceTextMarkers.Replace(text)
}
