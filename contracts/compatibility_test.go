package contracts

import (
	"encoding/json"
	"os"
	"testing"
)

func TestExtensionVersionCompatibility(t *testing.T) {
	data, err := os.ReadFile("testdata/compatibility.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Required, Provided string
		SDK, Plugin        bool
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Required+"/"+c.Provided, func(t *testing.T) {
			if got := SupportsVersion(c.Required, c.Provided); got != c.SDK {
				t.Errorf("SDK: %v, want %v", got, c.SDK)
			}
			if got := SupportsPluginAPIVersion(c.Required, c.Provided); got != c.Plugin {
				t.Errorf("Plugin: %v, want %v", got, c.Plugin)
			}
		})
	}
}
