//go:build !desktop

package decls

import (
	ts "github.com/amitbet/pr-manager/internal/sitter"
	"github.com/odvcencio/gotreesitter/grammars/bash"
	"github.com/odvcencio/gotreesitter/grammars/c"
	"github.com/odvcencio/gotreesitter/grammars/c_sharp"
	"github.com/odvcencio/gotreesitter/grammars/cpp"
	"github.com/odvcencio/gotreesitter/grammars/dart"
	"github.com/odvcencio/gotreesitter/grammars/java"
	"github.com/odvcencio/gotreesitter/grammars/kotlin"
	"github.com/odvcencio/gotreesitter/grammars/php"
	"github.com/odvcencio/gotreesitter/grammars/powershell"
	"github.com/odvcencio/gotreesitter/grammars/python"
	"github.com/odvcencio/gotreesitter/grammars/ruby"
	"github.com/odvcencio/gotreesitter/grammars/rust"
	"github.com/odvcencio/gotreesitter/grammars/scala"
	"github.com/odvcencio/gotreesitter/grammars/swift"
	"os"
)

func init() {
	// The gotreesitter heap check pauses every parser on large files.
	// Its per-parse arena and scratch budgets still bound memory use.
	if _, ok := os.LookupEnv("GOT_PARSE_MEMORY_HARD_CEILING_MB"); !ok {
		os.Setenv("GOT_PARSE_MEMORY_HARD_CEILING_MB", "0")
	}
}

func loadBash() *ts.Language   { return bash.Language() }
func loadC() *ts.Language      { return c.Language() }
func loadCSharp() *ts.Language { return c_sharp.Language() }
func loadCpp() *ts.Language {
	l := cpp.Language()
	l.CRecoveryCostCompetitionEnabledByDefault = ts.DiagnoseCRecoveryGate(l).Supported
	return l
}
func loadDart() *ts.Language       { return dart.Language() }
func loadJava() *ts.Language       { return java.Language() }
func loadKotlin() *ts.Language     { return kotlin.Language() }
func loadPHP() *ts.Language        { return php.Language() }
func loadPowershell() *ts.Language { return powershell.Language() }
func loadPython() *ts.Language     { return python.Language() }
func loadRuby() *ts.Language       { return ruby.Language() }
func loadRust() *ts.Language       { return rust.Language() }
func loadScala() *ts.Language      { return scala.Language() }
func loadSwift() *ts.Language      { return swift.Language() }
