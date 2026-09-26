package decls

import (
	"path"
	"strings"

	ts "github.com/amitbet/pr-triage/internal/sitter"
)

// The language specs of the generic parser. Each says which nodes declare
// what, how members nest, what is private, what a file imports, and what
// counts for complexity.

var genLangs = []*Lang{langSh, langPS, langC, langCpp, langPHP, langScala, langKotlin, langRuby, langSwift, langDart}

// --- shell ---

var langSh = &Lang{
	ID: "sh", Family: "sh", Exts: []string{".sh", ".bash"}, TypeSep: ".", MemberSep: ".",
	g: &grammar{load: loadBash},
	// A command name is one token (functions may have dashes); other words
	// and variables are strings, so only commands resolve.
	lex: genLex(&lexRules{
		comments: set("comment"),
		strings:  set("string", "raw_string", "ansi_c_string", "heredoc_body", "translated_string"),
		holes:    set("command_substitution", "process_substitution"),
	}, []string{"command_name"}, []string{"word", "variable_name", "special_variable_name", "extglob_pattern", "regex", "test_operator"}, nil),
	cx: &genCx{
		decide: set("if", "elif", "for", "while", "until", "case_item", "&&", "||"),
		nest:   set("if_statement", "for_statement", "c_style_for_statement", "while_statement", "case_statement", "subshell"),
		ifs:    set("if_statement"),
	},
	decl: func(t *tree, n *ts.Node, typ string) (string, string) {
		if typ == "function_definition" {
			return "function", fieldText(t, n, "name")
		}
		return "", ""
	},
	through:     set("ERROR", "if_statement", "compound_statement", "list"),
	importTypes: set("command"),
	imports: func(t *tree, n *ts.Node, _ string, f *GenFile) {
		name := strings.TrimSpace(fieldText(t, n, "name"))
		if name != "source" && name != "." {
			return
		}
		if a := t.field(n, "argument"); a != nil {
			if p := scriptPath(unquote(t.text(a))); p != "" {
				f.Imports = append(f.Imports, GenImport{Path: p, File: true})
			}
		}
	},
	test: func(p, b string) bool {
		return hasSuffixAny(b, "_test.sh", "_test.bash", ".test.sh") || strings.HasPrefix(b, "test_")
	},
}

// scriptPath turns a sourced path into a repo-relative one: a leading
// $DIR/ or $(dirname "$0")/ means the script's own directory.
func scriptPath(s string) string {
	if strings.HasPrefix(s, "$") || strings.HasPrefix(s, "(") {
		i := strings.IndexByte(s, '/')
		if i < 0 {
			return ""
		}
		s = "./" + s[i+1:]
	}
	if strings.ContainsAny(s, "$`*") || s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "./") && !strings.HasPrefix(s, "../") {
		s = "./" + s
	}
	return s
}

// --- PowerShell ---

var langPS = &Lang{
	ID: "ps1", Family: "ps1", Exts: []string{".ps1", ".psm1"}, TypeSep: ".", MemberSep: ".",
	Self: set("this"), VarPrefix: "$", CtorAlias: "new",
	g: &grammar{load: loadPowershell},
	lex: genLex(&lexRules{
		comments: set("comment"),
		strings: set("expandable_string_literal", "verbatim_string_characters", "expandable_here_string_literal",
			"verbatim_here_string_characters"),
		holes: set("sub_expression"),
	}, []string{"command_name", "function_name"}, []string{"generic_token", "command_parameter"}, nil),
	cx: &genCx{
		decide: set("if", "elseif", "for", "foreach", "while", "catch", "switch_clause", "-and", "-or"),
		nest: set("if_statement", "for_statement", "foreach_statement", "while_statement", "do_statement",
			"switch_statement", "try_statement", "script_block_expression"),
	},
	decl: func(t *tree, n *ts.Node, typ string) (string, string) {
		switch typ {
		case "function_statement":
			return "function", t.text(firstOf(t, n, "function_name"))
		case "class_statement":
			return "class", t.text(firstOf(t, n, "simple_name"))
		case "enum_statement":
			return "enum", t.text(firstOf(t, n, "simple_name"))
		case "class_method_definition":
			return "method", t.text(firstOf(t, n, "simple_name"))
		}
		return "", ""
	},
	body:    func(t *tree, n *ts.Node) *ts.Node { return n },
	through: set("statement_list", "ERROR"),
	supers: func(t *tree, n *ts.Node) []string {
		var out []string
		for i, c := range allOf(t, n, "simple_name") {
			if i > 0 {
				out = append(out, t.text(c))
			}
		}
		return out
	},
	private: func(t *tree, n *ts.Node, _ *GenDecl) bool {
		return modText(t, n, "class_attribute", "hidden")
	},
	importTypes: set("command"),
	imports: func(t *tree, n *ts.Node, _ string, f *GenFile) {
		name := t.field(n, "command_name")
		switch {
		case firstOf(t, n, "command_invokation_operator") != nil && t.typ(name) == "command_name_expr":
			if p := psPath(t.text(name)); p != "" {
				f.Imports = append(f.Imports, GenImport{Path: p, File: true})
			}
		case strings.EqualFold(strings.TrimSpace(t.text(name)), "Import-Module"):
			if a := find(t, t.field(n, "command_elements"), 2, "generic_token", "string_literal"); a != nil {
				if p := psPath(t.text(a)); p != "" && (strings.Contains(p, "/") || strings.Contains(p, ".ps")) {
					f.Imports = append(f.Imports, GenImport{Path: p, File: true})
				}
			}
		}
	},
	test: func(p, b string) bool { return hasSuffixAny(strings.ToLower(b), ".tests.ps1", ".test.ps1") },
}

func psPath(s string) string {
	s = strings.ReplaceAll(unquote(s), `\`, "/")
	for _, pre := range []string{"$PSScriptRoot/", "${PSScriptRoot}/", "$(Split-Path $MyInvocation.MyCommand.Path)/"} {
		if strings.HasPrefix(s, pre) {
			s = "./" + s[len(pre):]
		}
	}
	return scriptPath(s)
}

// --- C and C++ ---

var cDecide = []string{"if", "for", "while", "case", "&&", "||", "conditional_expression"}
var cNest = []string{"if_statement", "for_statement", "while_statement", "do_statement", "switch_statement"}

var cLex = &lexRules{
	comments: set("comment"),
	strings:  set("string_literal", "char_literal", "raw_string_literal", "system_lib_string"),
	node: func(t *tree, n *ts.Node, typ string) bool {
		return typ == "preproc_include" // no tokens for #include lines
	},
}

var cThrough = []string{"preproc_if", "preproc_ifdef", "preproc_else", "preproc_elif", "preproc_elifdef",
	"declaration", "linkage_specification", "declaration_list", "ERROR"}

func cDecl(t *tree, n *ts.Node, typ string, cxx bool) (string, string) {
	switch typ {
	case "function_definition":
		return "function", cFuncName(t, n)
	case "struct_specifier", "union_specifier", "enum_specifier", "class_specifier":
		if t.field(n, "body") == nil || t.field(n, "name") == nil {
			return "", ""
		}
		if typ == "class_specifier" && !cxx {
			return "", ""
		}
		return strings.TrimSuffix(typ, "_specifier"), stripTemplates(fieldText(t, n, "name"))
	case "type_definition":
		if d := t.field(n, "declarator"); d != nil && t.typ(d) == "type_identifier" {
			return "typedef", t.text(d)
		}
	case "preproc_function_def":
		return "macro", fieldText(t, n, "name")
	case "field_declaration":
		// A virtual member declared without a body (an interface method)
		// has no other definition to point at.
		if d := t.field(n, "declarator"); cxx && d != nil && t.typ(d) == "function_declarator" && t.hasWord(n, "", "virtual") {
			return "function", cFuncName(t, n)
		}
	}
	return "", ""
}

// cFuncName follows a function definition's declarators to its name.
func cFuncName(t *tree, n *ts.Node) string {
	d := t.field(n, "declarator")
	for depth := 0; d != nil && depth < 8; depth++ {
		switch t.typ(d) {
		case "function_declarator":
			inner := t.field(d, "declarator")
			switch t.typ(inner) {
			case "identifier", "field_identifier", "destructor_name", "operator_name":
				return stripSpace(t.text(inner))
			case "qualified_identifier":
				return stripTemplates(stripSpace(t.text(inner)))
			case "template_function":
				return fieldText(t, inner, "name")
			}
			return ""
		case "pointer_declarator", "reference_declarator", "parenthesized_declarator", "attributed_declarator":
			if in := t.field(d, "declarator"); in != nil {
				d = in
			} else if k := t.named(d); len(k) > 0 {
				d = k[len(k)-1]
			} else {
				return ""
			}
		default:
			return ""
		}
	}
	return ""
}

func cStatic(t *tree, n *ts.Node, owner *GenDecl) bool {
	return owner == nil && modText(t, n, "storage_class_specifier", "static")
}

func cImports(t *tree, n *ts.Node, _ string, f *GenFile) {
	if p := t.field(n, "path"); p != nil && t.typ(p) == "string_literal" {
		f.Imports = append(f.Imports, GenImport{Path: unquote(t.text(p)), File: true})
	}
}

func cTest(p, b string) bool {
	s := strings.TrimSuffix(b, path.Ext(b))
	return strings.HasSuffix(s, "_test") || strings.HasSuffix(s, "_unittest") || strings.HasSuffix(s, "_tests") ||
		strings.HasPrefix(s, "test_") || strings.HasSuffix(s, "Test") || strings.HasSuffix(s, "Tests")
}

var langC = &Lang{
	ID: "c", Family: "c", Exts: []string{".c"}, TypeSep: ".", MemberSep: ".", chunks: true,
	g: &grammar{load: loadC}, lex: cLex,
	cx:          &genCx{decide: set(cDecide...), nest: set(cNest...), ifs: set("if_statement")},
	decl:        func(t *tree, n *ts.Node, typ string) (string, string) { return cDecl(t, n, typ, false) },
	body:        func(*tree, *ts.Node) *ts.Node { return nil }, // C structs hold fields only
	through:     set(cThrough...),
	private:     cStatic,
	importTypes: set("preproc_include"), imports: cImports,
	test: cTest,
}

var langCpp = &Lang{
	ID: "cpp", Family: "c", Exts: []string{".cc", ".cpp", ".cxx", ".c++", ".h", ".hh", ".hpp", ".hxx", ".h++", ".ipp", ".tpp"},
	TypeSep: "::", MemberSep: "::", Self: set("this"), ImplicitSelf: true, OutOfLine: true, chunks: true,
	g: &grammar{load: cppLanguage}, lex: cLex,
	cx: &genCx{
		decide: set(append(cDecide, "catch")...),
		nest:   set(append(cNest, "for_range_loop", "try_statement", "lambda_expression")...),
		ifs:    set("if_statement"),
	},
	decl: func(t *tree, n *ts.Node, typ string) (string, string) { return cDecl(t, n, typ, true) },
	namespace: func(t *tree, n *ts.Node, typ string) (string, *ts.Node, bool) {
		if typ != "namespace_definition" {
			return "", nil, false
		}
		b := t.field(n, "body")
		if b == nil {
			return "", nil, false
		}
		return strings.ReplaceAll(stripSpace(fieldText(t, n, "name")), "::", "."), b, true
	},
	through: set(append(cThrough, "template_declaration", "field_declaration", "export_declaration")...),
	section: func(t *tree, n *ts.Node, typ string) (bool, bool) {
		if typ != "access_specifier" {
			return false, false
		}
		return strings.TrimSpace(t.text(n)) != "public", true
	},
	privateDefault: set("class"),
	supers: func(t *tree, n *ts.Node) []string {
		var out []string
		for _, b := range allOf(t, n, "base_class_clause") {
			for _, c := range allOf(t, b, "type_identifier", "qualified_identifier", "template_type") {
				out = append(out, stripTemplates(stripSpace(t.text(c))))
			}
		}
		return out
	},
	private:     cStatic,
	importTypes: set("preproc_include"), imports: cImports,
	test: cTest,
}

// The pure Go loader enables C++ error recovery for macro-heavy files.
// The cgo loader uses the C runtime's recovery behavior.
func cppLanguage() *ts.Language {
	return loadCpp()
}

// --- PHP ---

var langPHP = &Lang{
	ID: "php", Family: "php", Exts: []string{".php"}, TypeSep: "::", MemberSep: "::",
	Self: set("this", "self", "static"), Super: set("parent"), VarPrefix: "$",
	g: &grammar{load: loadPHP},
	lex: genLex(&lexRules{
		comments: set("comment"),
		strings:  set("string", "encapsed_string", "heredoc", "nowdoc", "shell_command_expression"),
	}, nil, []string{"text"}, []string{"php_tag", "text_interpolation"}),
	cx: &genCx{
		decide: set("if", "elseif", "for", "foreach", "while", "case", "catch", "&&", "||", "??", "and", "or",
			"conditional_expression", "match_conditional_expression"),
		nest: set("if_statement", "for_statement", "foreach_statement", "while_statement", "do_statement",
			"switch_statement", "try_statement", "match_expression", "anonymous_function", "anonymous_function_creation_expression",
			"arrow_function"),
		ifs: set("if_statement"),
	},
	decl: func(t *tree, n *ts.Node, typ string) (string, string) {
		kind := map[string]string{"class_declaration": "class", "interface_declaration": "interface",
			"trait_declaration": "trait", "enum_declaration": "enum", "method_declaration": "method",
			"function_definition": "function"}[typ]
		if kind == "" {
			return "", ""
		}
		return kind, fieldText(t, n, "name")
	},
	ctorName: "__construct",
	namespace: func(t *tree, n *ts.Node, typ string) (string, *ts.Node, bool) {
		if typ != "namespace_definition" {
			return "", nil, false
		}
		return strings.ReplaceAll(fieldText(t, n, "name"), `\`, "."), t.field(n, "body"), true
	},
	through: set("ERROR"),
	supers: func(t *tree, n *ts.Node) []string {
		var out []string
		for _, cl := range allOf(t, n, "base_clause", "class_interface_clause") {
			for _, c := range allOf(t, cl, "name", "qualified_name") {
				out = append(out, strings.TrimPrefix(t.text(c), `\`))
			}
		}
		if b := t.field(n, "body"); b != nil {
			for _, u := range allOf(t, b, "use_declaration") {
				for _, c := range allOf(t, u, "name", "qualified_name") {
					out = append(out, strings.TrimPrefix(t.text(c), `\`))
				}
			}
		}
		return out
	},
	private: func(t *tree, n *ts.Node, _ *GenDecl) bool {
		return modText(t, n, "visibility_modifier", "private", "protected")
	},
	importTypes: set("namespace_use_declaration", "include_expression", "include_once_expression", "require_expression", "require_once_expression"),
	imports: func(t *tree, n *ts.Node, typ string, f *GenFile) {
		if typ != "namespace_use_declaration" {
			if s := find(t, n, 3, "string", "encapsed_string"); s != nil {
				p := strings.TrimPrefix(unquote(t.text(s)), "/")
				if p != "" && !strings.Contains(p, "$") {
					f.Imports = append(f.Imports, GenImport{Path: "./" + p, File: true})
				}
			}
			return
		}
		php := func(s string) string { return strings.ReplaceAll(strings.TrimPrefix(stripSpace(s), `\`), `\`, ".") }
		// Older C grammars expose grouped use clauses as an error node.
		// Recover those imports from the declaration text.
		if raw := t.text(n); strings.Contains(raw, "{") {
			open, close := strings.IndexByte(raw, '{'), strings.LastIndexByte(raw, '}')
			if close > open {
				prefix := php(strings.TrimSpace(strings.TrimPrefix(raw[:open], "use ")))
				for _, part := range strings.Split(raw[open+1:close], ",") {
					part = strings.TrimSpace(part)
					alias := ""
					if before, after, ok := strings.Cut(part, " as "); ok {
						part, alias = before, strings.TrimSpace(after)
					}
					if part != "" {
						f.Imports = append(f.Imports, GenImport{Path: strings.TrimSuffix(prefix, ".") + "." + php(part), Alias: alias})
					}
				}
				return
			}
		}
		prefix := ""
		if g := t.field(n, "body"); g != nil {
			prefix = php(t.text(firstOf(t, n, "namespace_name"))) + "."
			n = g
		}
		for _, c := range t.named(n) {
			imp := GenImport{}
			switch t.typ(c) {
			case "namespace_use_clause":
				if a := t.field(c, "alias"); a != nil {
					imp.Alias = t.text(a)
				}
				if q := firstOf(t, c, "qualified_name", "name"); q != nil {
					imp.Path = prefix + php(t.text(q))
				}
			case "qualified_name", "name":
				imp.Path = prefix + php(t.text(c))
			}
			if imp.Path != "" && imp.Path != prefix {
				f.Imports = append(f.Imports, imp)
			}
		}
	},
	test: func(p, b string) bool { return strings.HasSuffix(b, "Test.php") },
}

// --- Scala ---

var langScala = &Lang{
	ID: "scala", Family: "scala", Exts: []string{".scala", ".sc"}, TypeSep: ".", MemberSep: ".", chunks: true,
	Self: set("this"), Super: set("super"), ImplicitSelf: true, ctorName: "apply",
	g: &grammar{load: loadScala},
	lex: &lexRules{
		comments: set("comment", "block_comment"),
		strings:  set("string", "interpolated_string_expression", "character_literal"),
		holes:    set("interpolation"),
	},
	cx: &genCx{
		decide: set("if", "while", "catch", "for"),
		arm: func(t *tree, n *ts.Node, typ string) bool {
			switch typ {
			case "operator_identifier":
				s := t.text(n)
				return s == "&&" || s == "||"
			case "case_clause":
				p := t.field(n, "pattern")
				return p == nil || t.typ(p) != "wildcard"
			}
			return false
		},
		nest: set("if_expression", "for_expression", "while_expression", "do_while_expression", "match_expression",
			"try_expression", "lambda_expression"),
		ifs: set("if_expression"),
	},
	decl: func(t *tree, n *ts.Node, typ string) (string, string) {
		kind := map[string]string{"class_definition": "class", "trait_definition": "trait", "object_definition": "object",
			"enum_definition": "enum", "function_definition": "function", "function_declaration": "function"}[typ]
		if kind == "" {
			return "", ""
		}
		return kind, fieldText(t, n, "name")
	},
	namespace: func(t *tree, n *ts.Node, typ string) (string, *ts.Node, bool) {
		if typ != "package_clause" {
			return "", nil, false
		}
		return stripSpace(fieldText(t, n, "name")), t.field(n, "body"), true
	},
	through: set("ERROR"),
	supers: func(t *tree, n *ts.Node) []string {
		var out []string
		if e := t.field(n, "extend"); e != nil {
			for _, c := range t.named(e) {
				if s := typeName(t.text(c)); s != "" && t.typ(c) != "arguments" {
					out = append(out, s)
				}
			}
		}
		return out
	},
	private: func(t *tree, n *ts.Node, _ *GenDecl) bool {
		return modText(t, n, "access_modifier", "private", "protected")
	},
	importTypes: set("import_declaration"),
	imports: func(t *tree, n *ts.Node, _ string, f *GenFile) {
		var segs []string
		kids, fields := t.children(n)
		for i, c := range kids {
			if fields[i] == "path" && c.IsNamed() {
				segs = append(segs, t.text(c))
			}
		}
		base := strings.Join(segs, ".")
		sel := false
		for _, c := range t.named(n) {
			switch t.typ(c) {
			case "namespace_wildcard":
				f.Imports = append(f.Imports, GenImport{Path: base, Wildcard: true})
				sel = true
			case "namespace_selectors":
				sel = true
				for _, s := range t.named(c) {
					switch t.typ(s) {
					case "identifier":
						f.Imports = append(f.Imports, GenImport{Path: base + "." + t.text(s)})
					case "arrow_renamed_identifier", "as_renamed_identifier":
						f.Imports = append(f.Imports, GenImport{Path: base + "." + fieldText(t, s, "name"), Alias: fieldText(t, s, "alias")})
					case "namespace_wildcard", "wildcard":
						f.Imports = append(f.Imports, GenImport{Path: base, Wildcard: true})
					}
				}
			}
		}
		if !sel && base != "" {
			f.Imports = append(f.Imports, GenImport{Path: base})
		}
	},
	test: func(p, b string) bool {
		return strings.Contains(p, "src/test/") || hasSuffixAny(b, "Spec.scala", "Test.scala", "Suite.scala", "Tests.scala")
	},
}

// --- Kotlin ---

var langKotlin = &Lang{
	ID: "kt", Family: "kt", Exts: []string{".kt"}, TypeSep: ".", MemberSep: ".", chunks: true,
	Self: set("this"), Super: set("super"), ImplicitSelf: true,
	g: &grammar{load: loadKotlin},
	lex: &lexRules{
		comments: set("line_comment", "multiline_comment", "comment"),
		strings:  set("string_literal", "character_literal", "multiline_string_literal"),
		holes:    set("interpolation"),
	},
	cx: &genCx{
		decide: set("if", "for", "while", "catch", "&&", "||", "?:"),
		arm: func(t *tree, n *ts.Node, typ string) bool {
			return typ == "when_entry" && !t.hasWord(n, "", "else")
		},
		nest: set("if_expression", "for_statement", "while_statement", "do_while_statement", "when_expression",
			"try_expression", "lambda_literal", "anonymous_function"),
		ifs: set("if_expression"),
	},
	decl: func(t *tree, n *ts.Node, typ string) (string, string) {
		switch typ {
		case "class_declaration":
			kind := "class"
			switch {
			case t.hasWord(n, "", "interface"):
				kind = "interface"
			case t.hasWord(n, "", "enum") || firstOf(t, n, "enum_class_body") != nil || modText(t, n, "class_modifier", "enum"):
				kind = "enum"
			}
			return kind, t.text(firstOf(t, n, "type_identifier"))
		case "object_declaration":
			return "object", t.text(firstOf(t, n, "type_identifier"))
		case "function_declaration":
			return "function", t.text(firstOf(t, n, "simple_identifier"))
		case "secondary_constructor":
			return "ctor", "constructor"
		case "property_declaration":
			if firstOf(t, n, "getter", "setter") != nil {
				return "property", t.text(find(t, firstOf(t, n, "variable_declaration"), 1, "simple_identifier"))
			}
		}
		return "", ""
	},
	body: func(t *tree, n *ts.Node) *ts.Node { return firstOf(t, n, "class_body", "enum_class_body") },
	namespace: func(t *tree, n *ts.Node, typ string) (string, *ts.Node, bool) {
		if typ != "package_header" {
			return "", nil, false
		}
		return stripSpace(t.text(firstOf(t, n, "identifier", "qualified_identifier"))), nil, true
	},
	through: set("ERROR", "companion_object", "class_body"),
	supers: func(t *tree, n *ts.Node) []string {
		var out []string
		var walk func(n *ts.Node)
		walk = func(n *ts.Node) {
			for _, c := range t.named(n) {
				switch t.typ(c) {
				case "delegation_specifiers":
					walk(c)
				case "delegation_specifier":
					if u := find(t, c, 2, "user_type"); u != nil {
						out = append(out, typeName(t.text(u)))
					}
				}
			}
		}
		walk(n)
		return out
	},
	private: func(t *tree, n *ts.Node, _ *GenDecl) bool {
		return modText(t, n, "visibility_modifier", "private", "protected", "internal")
	},
	importTypes: set("import_header"),
	imports: func(t *tree, n *ts.Node, _ string, f *GenFile) {
		imp := GenImport{Path: stripSpace(t.text(firstOf(t, n, "identifier", "qualified_identifier")))}
		imp.Wildcard = firstOf(t, n, "wildcard_import") != nil
		if a := firstOf(t, n, "import_alias"); a != nil {
			imp.Alias = t.text(find(t, a, 1, "type_identifier", "simple_identifier"))
		}
		if imp.Path != "" {
			f.Imports = append(f.Imports, imp)
		}
	},
	test: func(p, b string) bool {
		return strings.Contains(p, "src/test/") || hasSuffixAny(b, "Test.kt", "Tests.kt", "Spec.kt")
	},
}

// --- Ruby ---

var langRuby = &Lang{
	ID: "rb", Family: "rb", Exts: []string{".rb", ".rake"}, TypeSep: "::", MemberSep: ".",
	Self: set("self"), Super: set("super"), ImplicitSelf: true, CtorAlias: "new", ctorName: "initialize",
	g: &grammar{load: loadRuby},
	lex: genLex(&lexRules{
		comments: set("comment"),
		strings:  set("string", "heredoc_body", "subshell", "regex", "delimited_symbol", "string_array", "symbol_array", "character"),
		holes:    set("interpolation"),
	}, []string{"identifier"}, []string{"simple_symbol", "hash_key_symbol"}, nil),
	cx: &genCx{
		decide: set("if", "elsif", "unless", "while", "until", "for", "when", "rescue", "&&", "||", "and", "or", "?"),
		leaves: true,
		nest:   set("if", "unless", "while", "until", "for", "case", "case_match", "begin", "block", "do_block", "lambda"),
	},
	decl: func(t *tree, n *ts.Node, typ string) (string, string) {
		switch typ {
		case "class", "module":
			return typ, stripSpace(fieldText(t, n, "name"))
		case "method", "singleton_method":
			return "function", fieldText(t, n, "name")
		}
		return "", ""
	},
	through: set("ERROR", "body_statement", "singleton_class", "call", "argument_list", "do_block", "block", "begin"),
	section: func(t *tree, n *ts.Node, typ string) (bool, bool) {
		if typ != "identifier" {
			return false, false
		}
		switch t.text(n) {
		case "private", "protected":
			return true, true
		case "public":
			return false, true
		}
		return false, false
	},
	supers: func(t *tree, n *ts.Node) []string {
		var out []string
		if s := t.field(n, "superclass"); s != nil {
			if c := firstOf(t, s, "constant", "scope_resolution"); c != nil {
				out = append(out, stripSpace(t.text(c)))
			}
		}
		if b := t.field(n, "body"); b != nil {
			for _, c := range allOf(t, b, "call") {
				switch fieldText(t, c, "method") {
				case "include", "extend", "prepend":
					if a := t.field(c, "arguments"); a != nil {
						for _, k := range allOf(t, a, "constant", "scope_resolution") {
							out = append(out, stripSpace(t.text(k)))
						}
						if k := t.typ(a); k == "constant" || k == "scope_resolution" {
							out = append(out, stripSpace(t.text(a)))
						}
					}
				}
			}
		}
		return out
	},
	importTypes: set("call"),
	imports: func(t *tree, n *ts.Node, _ string, f *GenFile) {
		m := fieldText(t, n, "method")
		if m != "require" && m != "require_relative" && m != "load" || t.field(n, "receiver") != nil {
			return
		}
		s := find(t, t.field(n, "arguments"), 2, "string")
		if s == nil || find(t, s, 1, "interpolation") != nil {
			return
		}
		p := unquote(t.text(s))
		if !strings.HasSuffix(p, ".rb") {
			p += ".rb"
		}
		if m == "require_relative" && !strings.HasPrefix(p, "../") {
			p = "./" + p
		}
		f.Imports = append(f.Imports, GenImport{Path: p, File: true})
	},
	test: func(p, b string) bool {
		return hasSuffixAny(b, "_spec.rb", "_test.rb") || strings.HasPrefix(b, "test_")
	},
}

// --- Swift ---

var langSwift = &Lang{
	ID: "swift", Family: "swift", Exts: []string{".swift"}, TypeSep: ".", MemberSep: ".", chunks: true,
	Self: set("self"), Super: set("super"), ImplicitSelf: true, ctorName: "init",
	g: &grammar{load: loadSwift},
	lex: &lexRules{
		comments: set("comment", "multiline_comment"),
		strings:  set("line_string_literal", "multi_line_string_literal", "raw_string_literal"),
		holes:    set("interpolated_expression"),
	},
	cx: &genCx{
		decide: set("if", "guard", "for", "while", "case", "catch", "&&", "||", "??", "ternary_expression"),
		nest: set("if_statement", "guard_statement", "for_statement", "while_statement", "repeat_while_statement",
			"switch_statement", "do_statement", "lambda_literal"),
		ifs: set("if_statement"),
	},
	decl: func(t *tree, n *ts.Node, typ string) (string, string) {
		switch typ {
		case "class_declaration":
			kind := "class"
			for _, k := range []string{"struct", "enum", "extension", "actor"} {
				if t.hasWord(n, "", k) {
					kind = k
				}
			}
			return kind, typeName(fieldText(t, n, "name"))
		case "protocol_declaration":
			return "protocol", fieldText(t, n, "name")
		case "function_declaration", "protocol_function_declaration":
			kids, fields := t.children(n)
			for i, c := range kids {
				if fields[i] == "name" && t.typ(c) == "simple_identifier" {
					return "function", t.text(c)
				}
			}
		case "init_declaration":
			return "ctor", "init"
		case "deinit_declaration":
			return "method", "deinit"
		case "subscript_declaration":
			return "method", "subscript"
		case "property_declaration":
			if t.field(n, "computed_value") != nil {
				return "property", t.text(find(t, t.field(n, "name"), 2, "simple_identifier"))
			}
		}
		return "", ""
	},
	through: set("ERROR"),
	supers: func(t *tree, n *ts.Node) []string {
		var out []string
		for _, c := range allOf(t, n, "inheritance_specifier") {
			out = append(out, typeName(t.text(c)))
		}
		return out
	},
	private: func(t *tree, n *ts.Node, _ *GenDecl) bool {
		return !modText(t, n, "visibility_modifier", "public", "open")
	},
	importTypes: set("import_declaration"),
	imports: func(t *tree, n *ts.Node, _ string, f *GenFile) {
		if id := firstOf(t, n, "identifier", "simple_identifier"); id != nil {
			f.Imports = append(f.Imports, GenImport{Path: stripSpace(t.text(id)), Module: true})
		}
	},
	module: func(p string) string {
		if i := strings.Index("/"+p, "/Sources/"); i >= 0 {
			rest := p[i+len("Sources/"):]
			if j := strings.IndexByte(rest, '/'); j > 0 {
				return rest[:j]
			}
		}
		return ""
	},
	test: func(p, b string) bool { return hasSuffixAny(b, "Tests.swift", "Test.swift") },
}

// --- Dart ---

var langDart = &Lang{
	ID: "dart", Family: "dart", Exts: []string{".dart"}, TypeSep: ".", MemberSep: ".", chunks: true,
	Self: set("this"), Super: set("super"), ImplicitSelf: true,
	g: &grammar{load: loadDart},
	lex: &lexRules{
		comments: set("comment", "documentation_comment"),
		strings:  set("string_literal"),
		holes:    set("template_substitution"),
	},
	cx: &genCx{
		decide: set("if", "for", "while", "case", "catch", "&&", "||", "??", "conditional_expression"),
		nest: set("if_statement", "for_statement", "while_statement", "do_statement", "switch_statement",
			"switch_expression", "try_statement", "function_expression"),
		ifs: set("if_statement"),
	},
	decl:        dartDecl,
	bodySibling: set("function_body"),
	body: func(t *tree, n *ts.Node) *ts.Node {
		if b := t.field(n, "body"); b != nil {
			return b
		}
		return firstOf(t, n, "class_body", "extension_body", "enum_body")
	},
	through: set("ERROR"),
	supers: func(t *tree, n *ts.Node) []string {
		var out []string
		for _, cl := range allOf(t, n, "superclass", "interfaces", "mixins") {
			for _, c := range t.named(cl) {
				switch t.typ(c) {
				case "type_identifier":
					out = append(out, t.text(c))
				case "mixins":
					for _, m := range allOf(t, c, "type_identifier") {
						out = append(out, t.text(m))
					}
				}
			}
		}
		return out
	},
	private: func(t *tree, n *ts.Node, _ *GenDecl) bool {
		_, name := dartDecl(t, n, t.typ(n))
		return strings.HasPrefix(name, "_")
	},
	importTypes: set("import_specification", "import_or_export", "library_export", "part_directive", "part_of_directive"),
	imports: func(t *tree, n *ts.Node, typ string, f *GenFile) {
		if typ == "import_or_export" {
			return // its import_specification is visited
		}
		s := find(t, n, 4, "string_literal")
		if s == nil {
			return
		}
		p := unquote(t.text(s))
		switch {
		case strings.HasPrefix(p, "dart:"):
			return
		case strings.HasPrefix(p, "package:"):
			if i := strings.IndexByte(p, '/'); i > 0 {
				p = "lib/" + p[i+1:] // resolved by suffix, so any package in the repo matches
			}
		case !strings.HasPrefix(p, "../") && !strings.HasPrefix(p, "/"):
			p = "./" + p
		}
		imp := GenImport{Path: p, File: true}
		kids := t.named(n)
		for i, c := range kids {
			if t.typ(c) == "identifier" && i > 0 {
				imp.Alias = t.text(c)
				break
			}
		}
		f.Imports = append(f.Imports, imp)
	},
	test: func(p, b string) bool { return strings.HasSuffix(b, "_test.dart") },
	generated: func(p string) bool {
		return hasSuffixAny(p, ".g.dart", ".freezed.dart", ".mocks.dart", ".gr.dart", ".pb.dart", ".pbenum.dart", ".pbjson.dart")
	},
}

func dartDecl(t *tree, n *ts.Node, typ string) (string, string) {
	switch typ {
	case "class_definition", "extension_type_declaration":
		return "class", fieldText(t, n, "name")
	case "enum_declaration":
		return "enum", fieldText(t, n, "name")
	case "mixin_declaration":
		if s := fieldText(t, n, "name"); s != "" {
			return "mixin", s
		}
		return "mixin", t.text(firstOf(t, n, "identifier"))
	case "extension_declaration":
		if s := fieldText(t, n, "name"); s != "" {
			return "extension", s
		}
		return "extension", typeName(fieldText(t, n, "class"))
	case "method_signature":
		if k := t.named(n); len(k) > 0 {
			return dartDecl(t, k[0], t.typ(k[0]))
		}
	case "declaration":
		if c := firstOf(t, n, "constructor_signature", "constant_constructor_signature", "factory_constructor_signature",
			"redirecting_factory_constructor_signature", "function_signature", "getter_signature", "setter_signature"); c != nil {
			return dartDecl(t, c, t.typ(c))
		}
	case "function_signature":
		return "function", fieldText(t, n, "name")
	case "getter_signature", "setter_signature":
		return "property", fieldText(t, n, "name")
	case "constructor_signature", "constant_constructor_signature", "factory_constructor_signature",
		"redirecting_factory_constructor_signature":
		ids := allOf(t, n, "identifier")
		if len(ids) == 0 {
			return "", ""
		}
		return "ctor", t.text(ids[len(ids)-1])
	case "operator_signature":
		if op := firstOf(t, n, "binary_operator", "unary_operator", "operator"); op != nil {
			return "method", "operator" + stripSpace(t.text(op))
		}
		return "method", "operator"
	}
	return "", ""
}
