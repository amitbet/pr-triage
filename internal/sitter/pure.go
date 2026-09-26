//go:build !desktop

package sitter

import ts "github.com/odvcencio/gotreesitter"

const Backend = "gotreesitter"

type Language = ts.Language
type Node = ts.Node
type ParserPool = ts.ParserPool
type ParserPoolOption = ts.ParserPoolOption

var NewParserPool = ts.NewParserPool
var WithParserPoolTimeoutMicros = ts.WithParserPoolTimeoutMicros

func DiagnoseCRecoveryGate(l *Language) ts.CRecoveryGateDiagnostics {
	return ts.DiagnoseCRecoveryGate(l)
}
