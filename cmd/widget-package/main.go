// widget-package validates a Hub source package with the same contract as installation.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/widget"
)

func main() {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, int64(contracts.Widget().Distribution.MaxBytes)+1))
	if err == nil {
		_, hash, e := widget.DecodeDistribution(data, fmt.Sprintf("%x", sha256.Sum256(data)))
		err = e
		if err == nil {
			err = json.NewEncoder(os.Stdout).Encode(map[string]string{"sourceHash": hash})
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
