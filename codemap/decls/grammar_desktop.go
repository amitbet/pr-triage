//go:build desktop

package decls

import (
	dart "github.com/UserNobody14/tree-sitter-dart/bindings/go"
	"github.com/amitbet/pr-manager/internal/powershell"
	ts "github.com/amitbet/pr-manager/internal/sitter"
	"github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/bash"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/scala"
	"github.com/smacker/go-tree-sitter/swift"
)

func loadBash() *ts.Language       { return ts.WrapLanguage(bash.GetLanguage()) }
func loadC() *ts.Language          { return ts.WrapLanguage(c.GetLanguage()) }
func loadCSharp() *ts.Language     { return ts.WrapLanguage(csharp.GetLanguage()) }
func loadCpp() *ts.Language        { return ts.WrapLanguage(cpp.GetLanguage()) }
func loadDart() *ts.Language       { return ts.WrapLanguage(sitter.NewLanguage(dart.Language())) }
func loadJava() *ts.Language       { return ts.WrapLanguage(java.GetLanguage()) }
func loadKotlin() *ts.Language     { return ts.WrapLanguage(kotlin.GetLanguage()) }
func loadPHP() *ts.Language        { return ts.WrapLanguage(php.GetLanguage()) }
func loadPowershell() *ts.Language { return ts.WrapLanguage(sitter.NewLanguage(powershell.Language())) }
func loadPython() *ts.Language     { return ts.WrapLanguage(python.GetLanguage()) }
func loadRuby() *ts.Language       { return ts.WrapLanguage(ruby.GetLanguage()) }
func loadRust() *ts.Language       { return ts.WrapLanguage(rust.GetLanguage()) }
func loadScala() *ts.Language      { return ts.WrapLanguage(scala.GetLanguage()) }
func loadSwift() *ts.Language      { return ts.WrapLanguage(swift.GetLanguage()) }
