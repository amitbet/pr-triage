//go:build desktop

package powershell

// The grammar is pinned to airbus-cert/tree-sitter-powershell commit fb1e1fe7e1fc.
// The upstream Go binding lacks the scanner include and include path.

// #cgo CFLAGS: -std=c11 -I${SRCDIR}
// #include "parser.inc"
// #include "scanner.inc"
// #include <stdbool.h>
// typedef struct TSParser TSParser;
// TSParser *ts_parser_new(void);
// bool ts_parser_set_language(TSParser *, const TSLanguage *);
// void ts_parser_delete(TSParser *);
import "C"

import "unsafe"

func Language() unsafe.Pointer {
	lang := C.tree_sitter_powershell()
	parser := C.ts_parser_new()
	if parser == nil {
		panic("tree-sitter: failed to create parser")
	}
	compatible := C.ts_parser_set_language(parser, lang)
	C.ts_parser_delete(parser)
	if !bool(compatible) {
		panic("tree-sitter: incompatible PowerShell grammar")
	}
	return unsafe.Pointer(lang)
}
