package pluginexec

import (
	"github.com/teatak/pudding-core/internal/plugin"
	"testing"
)

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
