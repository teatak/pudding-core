package contracts

import (
	"net/url"
	"strings"
)

// Preserve existing official install ownership while catalogs move behind the
// root index. Third-party index references keep their own index URL and ID.
func CatalogSourceIdentity(kind, raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	u.Host = strings.ToLower(u.Host)
	if u.Scheme == "https" && u.Port() == "443" {
		u.Host = strings.TrimSuffix(u.Host, ":443")
	}
	normalized := u.String()
	base := "https://teatak.github.io/pudding-hub/"
	folder := "widgets"
	if kind == "plugin" {
		folder = "plugins"
	}
	identity := base + folder + "/registry.json"
	if normalized == identity || normalized == base+"extensions/"+folder+"/registry.json" || normalized == base+folder+"/registry-v2.json" {
		return identity
	}
	if u.Scheme == "https" && u.Host == "teatak.github.io" && u.Path == "/pudding-hub/index.json" && u.User == nil && u.Fragment == "" && len(u.Query()) == 1 && len(u.Query()["source"]) == 1 && u.Query().Get("source") == "teatak/pudding-hub" {
		return identity
	}
	return normalized
}
