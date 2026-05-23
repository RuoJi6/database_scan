package app

import (
	"database_scan/internal/scanner"
	"database_scan/internal/textfix"
)

func RepairStateText(state *ScanJobState) {
	scanner.RepairResultText(&state.Result, state.Request.TextEncoding)
	if state.SQLResult == nil {
		return
	}
	for i := range state.SQLResult.Rows {
		for j := range state.SQLResult.Rows[i] {
			state.SQLResult.Rows[i][j] = textfix.RepairString(state.SQLResult.Rows[i][j], state.Request.TextEncoding)
		}
	}
}
