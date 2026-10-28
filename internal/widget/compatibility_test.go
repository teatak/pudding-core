package widget

import (
	"errors"
	"github.com/teatak/pudding-core/contracts"
	"strings"
	"testing"
)

func TestPackageSDKCompatibility(t *testing.T) {
	for _, version := range []string{"1", "1.0.0", contracts.Widget().SDKVersion, "1.99.0", "2.0.0", "bad"} {
		t.Run(version, func(t *testing.T) {
			p := fixturePackage(t)
			p.Files["widget.json"] = strings.Replace(p.Files["widget.json"], `"sdkVersion":"1"`, `"sdkVersion":"`+version+`"`, 1)
			_, _, err := p.Validate()
			if contracts.SupportsVersion(version, contracts.Widget().SDKVersion) {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrIncompatibleSDK) {
				t.Fatalf("want incompatible SDK error, got %v", err)
			}
		})
	}
}

func TestInstalledWidgetWithUnsupportedSDKCannotBeReadOrExecuted(t *testing.T) {
	p := fixturePackage(t)
	p.Files["widget.json"] = strings.Replace(p.Files["widget.json"], `"sdkVersion":"1"`, `"sdkVersion":"2.0.0"`, 1)
	root := t.TempDir()
	hash := PackageHash(p.Files)
	parent, err := RevisionsDir(root, "sample")
	if err != nil {
		t.Fatal(err)
	}
	// A revision written by another SDK must remain intact, but cannot execute here.
	if err := WriteVersion(parent, hash, p.Files); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPackage(root, "sample", hash); !errors.Is(err, ErrIncompatibleSDK) {
		t.Fatal(err)
	}
	if got, err := ReadVersion(parent+"/"+hash, ValidFilePath); err != nil || got["widget.json"] != p.Files["widget.json"] {
		t.Fatal("installed files were not preserved", err)
	}
}
