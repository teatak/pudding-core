package contracts

import (
	_ "embed"
	"encoding/json"
)

//go:embed studio.json
var studioJSON []byte

type StudioPolicy struct {
	MaxTableRows       int `json:"maxTableRows"`
	MaxTableColumns    int `json:"maxTableColumns"`
	MaxTableBytes      int `json:"maxTableBytes"`
	MaxTableCellBytes  int `json:"maxTableCellBytes"`
	MaxTableOperations int `json:"maxTableOperations"`
	MaxTableReadRows   int `json:"maxTableReadRows"`
	MaxTableReadBytes  int `json:"maxTableReadBytes"`
	MaxDocumentBytes   int `json:"maxDocumentBytes"`
	MaxAssetBytes      int `json:"maxAssetBytes"`
	MaxDocumentEdits   int `json:"maxDocumentEdits"`
	MaxReadBytes       int `json:"maxReadBytes"`
	AutosaveMS         int `json:"autosaveMS"`
}

var studioPolicy = func() StudioPolicy {
	var p StudioPolicy
	if err := json.Unmarshal(studioJSON, &p); err != nil {
		panic(err)
	}
	if p.MaxTableRows < 1 || p.MaxTableColumns < 1 || p.MaxTableBytes < 1 || p.MaxTableCellBytes < 1 || p.MaxTableOperations < 1 || p.MaxTableReadRows < 1 || p.MaxTableReadBytes < 1 || p.MaxDocumentBytes < 1 || p.MaxAssetBytes < 1 || p.MaxDocumentEdits < 1 || p.MaxReadBytes < 4 || p.AutosaveMS < 1 {
		panic("invalid studio policy")
	}
	return p
}()

func Studio() StudioPolicy { return studioPolicy }
