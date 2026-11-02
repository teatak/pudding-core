package pluginexec

import (
	"encoding/base64"
	"github.com/teatak/pudding-core/internal/plugin"
	"testing"
)

func TestOnlyNativeEmailReadReceiptsCanSkipWidgetWriteConfirmation(t *testing.T) {
	id := base64.RawURLEncoding.EncodeToString([]byte(`{"f":"INBOX","v":1,"u":1}`))
	for _, tt := range []struct {
		name, adapter, method, path string
		body                        any
		allowed                     bool
	}{
		{"read receipt", "email", "POST", "/messages/" + id + "/read", nil, true},
		{"ordinary endpoint", "", "POST", "/messages/" + id + "/read", nil, false},
		{"read only query", "email", "GET", "/messages/" + id + "/read", nil, false},
		{"send", "email", "POST", "/send", nil, false},
		{"delete", "email", "DELETE", "/messages/" + id, nil, false},
		{"malformed id", "email", "POST", "/messages/not-a-message/read", nil, false},
		{"path traversal", "email", "POST", "/messages/../send/read", nil, false},
		{"extra body", "email", "POST", "/messages/" + id + "/read", map[string]any{"flags": []string{"\\Deleted"}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			binding := &plugin.EndpointBinding{Endpoint: plugin.Endpoint{Kind: "rest", Adapter: tt.adapter}}
			if got := IsEmailReadReceipt(binding, tt.method, tt.path, tt.body); got != tt.allowed {
				t.Fatalf("automatic read receipt = %v, want %v", got, tt.allowed)
			}
		})
	}
}

func TestEndpointURLPreservesEncodedSegment(t *testing.T) {
	u, err := BuildEndpointURL("https://fixture.test/api", "/projects/owner%2Frepo")
	if err != nil {
		t.Fatal(err)
	}
	if u.String() != "https://fixture.test/api/projects/owner%2Frepo" {
		t.Fatalf("encoded identifier changed meaning: %s", u)
	}
}
func TestWidgetCannotOverrideConnectionOwnedHotel(t *testing.T) {
	binding := &plugin.EndpointBinding{ConnectionFields: map[string]string{"hotelCode": "selected-hotel"}, ConnectionFieldDefs: []plugin.ConnectionField{{ID: "hotelCode", Inject: []plugin.ConnectionFieldInject{{Target: "body", Methods: []string{"POST"}}, {Target: "query", Methods: []string{"GET"}}}}}}
	if err := ValidateBoundRequest(binding, "POST", nil, map[string]any{"hotelCode": "other-hotel"}); err == nil {
		t.Fatal("body override accepted")
	}
	if err := ValidateBoundRequest(binding, "GET", map[string]any{"hotelCode": "other-hotel"}, nil); err == nil {
		t.Fatal("query override accepted")
	}
	if err := ValidateBoundRequest(binding, "POST", nil, map[string]any{"pageNo": float64(1)}); err != nil {
		t.Fatal(err)
	}
}
