package canvaslegacy

import (
	"errors"
	"path/filepath"
	"regexp"

	"github.com/teatak/pudding-core/internal/widget"
)

// The schema v25 upgrade writes the layout of its own release: canvas.json at
// <home>/canvases/<id>/revisions/<hash>. Schema v27 then moves every package to
// the widget layout, so this output must not follow later format changes.
const legacyManifest = "canvas.json"

var legacyID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,79}$`)

// WritePackage installs one converted package in the v25 layout.
func WritePackage(home, id string, p widget.Package) (string, error) {
	if err := validate(p); err != nil {
		return "", err
	}
	if home == "" || !legacyID.MatchString(id) {
		return "", errors.New("invalid canvas home or ID")
	}
	hash := widget.PackageHash(p.Files)
	return hash, widget.WriteVersion(filepath.Join(home, "canvases", id, "revisions"), hash, p.Files)
}

// validate applies the current package checks to the same files under the
// current manifest name; the v25 content model and limits are unchanged.
func validate(p widget.Package) error {
	view := make(map[string]string, len(p.Files))
	for name, content := range p.Files {
		if name == legacyManifest {
			name = widget.ManifestFile
		}
		view[name] = content
	}
	_, _, err := widget.Package{Files: view}.Validate()
	return err
}
