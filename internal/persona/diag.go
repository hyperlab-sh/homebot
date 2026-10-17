package persona

import (
	"fmt"

	"github.com/hyperlab-sh/homebot/internal/msg"
)

// diagnosticText keeps validator words in the generated message catalog.
var diagnosticText = map[string]func(string) msg.Text{
	"v0": msg.PersonaDiagV0,
	"v1": msg.PersonaDiagV1,
	"v2": msg.PersonaDiagV2,
	"v3": msg.PersonaDiagV3,
	"v4": msg.PersonaDiagV4,
	"v5": msg.PersonaDiagV5,
	"v6": msg.PersonaDiagV6,
	"v7": msg.PersonaDiagV7,
	"v9": msg.PersonaDiagV9,
	// V9 for a chain, reported against the file whose extends line is the
	// problem: a cycle, or a fifth file.
	"v9_cycle": msg.PersonaDiagV9Cycle,
	"v9_depth": msg.PersonaDiagV9Depth,
	"v11":      msg.PersonaDiagV11,
	"v12":      msg.PersonaDiagV12,
	"v13":      msg.PersonaDiagV13,
	"v14":      msg.PersonaDiagV14,
	"v15":      msg.PersonaDiagV15,
	"v16":      msg.PersonaDiagV16,
	"v17":      msg.PersonaDiagV17,
	"v18":      msg.PersonaDiagV18,
	"v19":      msg.PersonaDiagV19,
	"v20":      msg.PersonaDiagV20,
	"v21":      msg.PersonaDiagV21,
	"v22":      msg.PersonaDiagV22,
	"v23":      msg.PersonaDiagV23,
	"v24":      msg.PersonaDiagV24,
	"v26":      msg.PersonaDiagV26,
	"v28":      msg.PersonaDiagV28,
	"w1":       msg.PersonaDiagW1,
	"w2":       msg.PersonaDiagW2,
	"w4":       msg.PersonaDiagW4,
	"w5":       msg.PersonaDiagW5,
	"w6":       msg.PersonaDiagW6,
	"w7":       msg.PersonaDiagW7,
}

func diagnostic(code, detail string, line, col int, id string, variant int) Diagnostic {
	severity := Error
	if code[0] == 'w' {
		severity = Warning
	}
	return Diagnostic{Severity: severity, Line: line, Col: col, LineID: id, Variant: variant,
		Text: diagnosticText[code](fmt.Sprintf("%q", msg.StripUnsafe(detail)))}
}
