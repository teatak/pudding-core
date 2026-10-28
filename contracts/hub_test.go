package contracts

import "testing"

func TestCatalogSourceIdentitySurvivesOfficialPathMigration(t *testing.T) {
	for _, kind := range []string{"widget", "plugin"} {
		folder := "widgets"
		if kind == "plugin" {
			folder = "plugins"
		}
		expected := "https://teatak.github.io/pudding-hub/" + folder + "/registry.json"
		for _, url := range []string{expected, "https://teatak.github.io/pudding-hub/extensions/" + folder + "/registry.json", "https://teatak.github.io/pudding-hub/index.json?source=teatak%2Fpudding-hub"} {
			if got := CatalogSourceIdentity(kind, url); got != expected {
				t.Fatalf("%s: got %s", url, got)
			}
		}
		for _, url := range []string{"https://third.test/index.json?source=teatak%2Fpudding-hub", "https://teatak.github.io/pudding-hub/index.json?source=other%2Fhub"} {
			if CatalogSourceIdentity(kind, url) == expected {
				t.Fatalf("other source adopted official identity: %s", url)
			}
		}
	}
}
