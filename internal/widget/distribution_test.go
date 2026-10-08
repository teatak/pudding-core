package widget

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/teatak/pudding-core/contracts"
	"strings"
	"testing"
)

func TestDistributionRejectsTamperingAndUnsupportedHost(t *testing.T) {
	makePackage := func() Distribution {
		var d Distribution
		d.Kind = "pudding.widget.source-package"
		d.SchemaVersion = 2
		d.ID = "test/hub/widgets/todo"
		d.Version = "1.0.0"
		d.Title = map[string]string{"en": "Todo"}
		d.Description = map[string]string{}
		d.Requires.ProtocolVersion = 18
		d.Requires.SDKVersion = "1"
		d.Source.Files = map[string]string{"widget.json": `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{},"operations":{}}`, "src/App.tsx": "export default ()=>null"}
		d.FileHashes = map[string]string{}
		for name, value := range d.Source.Files {
			d.FileHashes[name] = fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
		}
		return d
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Distribution)
		ok     bool
	}{
		{"valid", func(*Distribution) {}, true},
		{"SVG icon", func(d *Distribution) {
			d.Icon = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="8"/></svg>`))
		}, true},
		{"remote icon", func(d *Distribution) { d.Icon = "https://hub.test/icon.svg" }, false},
		{"bad base64", func(d *Distribution) { d.Icon = "data:image/svg+xml;base64,???" }, false},
		{"HTML icon", func(d *Distribution) {
			d.Icon = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<html></html>`))
		}, false},
		{"broken SVG", func(d *Distribution) {
			d.Icon = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><g></svg>`))
		}, false},
		{"large icon", func(d *Distribution) {
			d.Icon = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(strings.Repeat(" ", contracts.Widget().Distribution.MaxIconBytes+1)))
		}, false},
		{"old HTML", func(d *Distribution) { d.Kind = "pudding.widget.package" }, false},
		{"new host", func(d *Distribution) { d.Requires.ProtocolVersion = 999 }, false},
		{"old host", func(d *Distribution) { d.Requires.ProtocolVersion = 17 }, false},
		{"SDK", func(d *Distribution) { d.Requires.SDKVersion = "999" }, false},
		{"traversal", func(d *Distribution) { d.Source.Files["../escape.ts"] = "x" }, false},
		{"changed file", func(d *Distribution) { d.Source.Files["src/App.tsx"] += "//edit" }, false},
		{"extra inventory", func(d *Distribution) { d.FileHashes["hidden.ts"] = "x" }, false},
		{"identity", func(d *Distribution) { d.ID = "../todo" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := makePackage()
			tc.mutate(&d)
			raw, _ := json.Marshal(d)
			_, hash, err := DecodeDistribution(raw, fmt.Sprintf("%x", sha256.Sum256(raw)))
			if (err == nil) != tc.ok {
				t.Fatalf("hash %s error %v", hash, err)
			}
		})
	}
}
