package contracts

import (
	_ "embed"
	"encoding/json"
)

//go:embed studio.json
var studioJSON []byte

type StudioPolicy struct {
	MaxSheetRows       int `json:"maxSheetRows"`
	MaxSheetColumns    int `json:"maxSheetColumns"`
	MaxSheetBytes      int `json:"maxSheetBytes"`
	MaxSheetCellBytes  int `json:"maxSheetCellBytes"`
	MaxSheetOperations int `json:"maxSheetOperations"`
	MaxSheetReadRows   int `json:"maxSheetReadRows"`
	MaxSheetReadBytes  int `json:"maxSheetReadBytes"`
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
	if p.MaxSheetRows < 1 || p.MaxSheetColumns < 1 || p.MaxSheetBytes < 1 || p.MaxSheetCellBytes < 1 || p.MaxSheetOperations < 1 || p.MaxSheetReadRows < 1 || p.MaxSheetReadBytes < 1 || p.MaxDocumentBytes < 1 || p.MaxAssetBytes < 1 || p.MaxDocumentEdits < 1 || p.MaxReadBytes < 4 || p.AutosaveMS < 1 {
		panic("invalid studio policy")
	}
	return p
}()

func Studio() StudioPolicy { return studioPolicy }
