package contracts

import (
	_ "embed"
	"encoding/json"
)

//go:embed studio.json
var studioJSON []byte

type StudioPolicy struct {
	MaxDocumentBytes int `json:"maxDocumentBytes"`
	MaxAssetBytes    int `json:"maxAssetBytes"`
	MaxDocumentEdits int `json:"maxDocumentEdits"`
	MaxReadBytes     int `json:"maxReadBytes"`
	AutosaveMS       int `json:"autosaveMS"`
}

var studioPolicy = func() StudioPolicy {
	var p StudioPolicy
	if err := json.Unmarshal(studioJSON, &p); err != nil {
		panic(err)
	}
	if p.MaxDocumentBytes < 1 || p.MaxAssetBytes < 1 || p.MaxDocumentEdits < 1 || p.MaxReadBytes < 4 || p.AutosaveMS < 1 {
		panic("invalid studio policy")
	}
	return p
}()

func Studio() StudioPolicy { return studioPolicy }
