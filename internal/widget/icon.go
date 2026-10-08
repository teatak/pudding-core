package widget

import (
	"encoding/base64"
	"encoding/xml"
	"strings"

	"github.com/teatak/pudding-core/contracts"
)

// ValidIconImage accepts self-contained SVG images. Clients must display them
// in an image context, where scripts and external resources cannot execute.
func ValidIconImage(icon string) bool {
	const prefix = "data:image/svg+xml;base64,"
	limit := contracts.Widget().Distribution.MaxIconBytes
	if !strings.HasPrefix(icon, prefix) || len(icon) > len(prefix)+base64.StdEncoding.EncodedLen(limit) {
		return false
	}
	data, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(icon, prefix))
	if err != nil || len(data) == 0 || len(data) > limit {
		return false
	}
	var svg struct {
		XMLName xml.Name `xml:"http://www.w3.org/2000/svg svg"`
	}
	return xml.Unmarshal(data, &svg) == nil
}
