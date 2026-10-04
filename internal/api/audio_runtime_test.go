package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/teatak/pudding-core/internal/audio/runtimeassets"
)

func TestMissingVoiceRuntimeCannotOfferModelInstallation(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, tc := range []struct{ method, route string }{
		{http.MethodGet, "/settings/audio/runtime"},
		{http.MethodPost, "/settings/audio/runtime/install"},
		{http.MethodPost, "/settings/audio/runtime/cancel"},
	} {
		req, _ := http.NewRequest(tc.method, srv.URL+tc.route, nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var status runtimeassets.Status
		err = json.NewDecoder(res.Body).Decode(&status)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusOK || !status.OK || !status.Disabled || status.Installed || status.Running || status.State != "unsupported" || status.Required == nil || status.Missing == nil {
			t.Fatalf("%s %s returned %d %+v", tc.method, tc.route, res.StatusCode, status)
		}
	}
}
