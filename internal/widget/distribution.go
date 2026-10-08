package widget

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"

	"github.com/teatak/pudding-core/contracts"
)

// Distribution is the Hub envelope. Source remains the same editable package used by authoring.
type Distribution struct {
	Kind          string            `json:"kind"`
	SchemaVersion int               `json:"schemaVersion"`
	ID            string            `json:"id"`
	Version       string            `json:"version"`
	Title         map[string]string `json:"title"`
	Icon          string            `json:"icon,omitempty"`
	Description   map[string]string `json:"description"`
	Requires      struct {
		ProtocolVersion int    `json:"protocolVersion"`
		SDKVersion      string `json:"sdkVersion"`
	} `json:"requires"`
	Source     Package           `json:"source"`
	FileHashes map[string]string `json:"fileHashes"`
}

var distributionID = regexp.MustCompile(`^[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+/widgets/[a-z][a-z0-9-]{0,63}$`)
var distributionVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-zA-Z0-9][a-zA-Z0-9.-]*)?$`)

func DecodeDistribution(data []byte, expectedHash string) (Distribution, string, error) {
	var d Distribution
	policy := contracts.Widget()
	if len(data) > policy.Distribution.MaxBytes || !revisionID.MatchString(expectedHash) {
		return d, "", errors.New("invalid widget package size or hash")
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != expectedHash {
		return d, "", errors.New("widget package hash mismatch")
	}
	if err := DecodeStrict(data, &d); err != nil {
		return d, "", err
	}
	if d.Kind != policy.Distribution.Kind || d.SchemaVersion != policy.Distribution.SchemaVersion {
		return d, "", errors.New("unsupported widget package format")
	}
	if !distributionID.MatchString(d.ID) || len(d.ID) > 240 || !distributionVersion.MatchString(d.Version) || len(d.Version) > 80 {
		return d, "", errors.New("invalid widget package identity")
	}
	if d.Requires.ProtocolVersion < policy.Distribution.MinProtocolVersion || d.Requires.ProtocolVersion > contracts.Runtime().ProtocolVersion || d.Requires.SDKVersion != policy.SDKVersion {
		return d, "", errors.New("unsupported widget host requirements")
	}
	if len(d.Title) == 0 || len(d.Title) > 20 || len(d.Description) > 20 {
		return d, "", errors.New("invalid widget metadata")
	}
	for _, title := range d.Title {
		if len(title) == 0 || len(title) > 200 {
			return d, "", errors.New("invalid widget title")
		}
	}
	for _, desc := range d.Description {
		if len(desc) > 2000 {
			return d, "", errors.New("invalid widget description")
		}
	}
	if d.Icon != "" && !ValidIconImage(d.Icon) {
		return d, "", errors.New("invalid widget icon")
	}
	_, sourceHash, err := d.Source.Validate()
	if err != nil {
		return d, "", err
	}
	if len(d.FileHashes) != len(d.Source.Files) {
		return d, "", errors.New("invalid widget file inventory")
	}
	for name, content := range d.Source.Files {
		h := sha256.Sum256([]byte(content))
		if d.FileHashes[name] != fmt.Sprintf("%x", h) {
			return d, "", fmt.Errorf("widget file hash mismatch: %s", name)
		}
	}
	return d, sourceHash, nil
}
