package main

import (
	"bytes"
	"fmt"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
)

// unifiedDiff produces unified diff format between oldData (labeled labelOld)
// and newData (labeled labelNew). If oldData and newData are identical, it returns "".
func unifiedDiff(labelOld string, oldData []byte, labelNew string, newData []byte) string {
	if bytes.Equal(oldData, newData) {
		return ""
	}
	if len(oldData) == 0 {
		labelOld = "/dev/null"
	}
	if len(newData) == 0 {
		labelNew = "/dev/null"
	}
	edits := myers.ComputeEdits("", string(oldData), string(newData))
	unified := gotextdiff.ToUnified(labelOld, labelNew, string(oldData), edits)
	return fmt.Sprint(unified)
}
