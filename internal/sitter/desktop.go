//go:build desktop

package sitter

import (
	"context"
	"fmt"

	ts "github.com/smacker/go-tree-sitter"
)

const Backend = "tree-sitter-cgo"

type Language struct {
	Native      *ts.Language
	SymbolNames []string
}

func WrapLanguage(l *ts.Language) *Language {
	if l == nil {
		return nil
	}
	names := make([]string, l.SymbolCount())
	for i := range names {
		names[i] = l.SymbolName(ts.Symbol(i))
	}
	return &Language{Native: l, SymbolNames: names}
}

type Node struct{ native *ts.Node }
type Point = ts.Point

func wrap(n *ts.Node) *Node {
	if n == nil {
		return nil
	}
	return &Node{n}
}
func (n *Node) ChildCount() int   { return int(n.native.ChildCount()) }
func (n *Node) Child(i int) *Node { return wrap(n.native.Child(i)) }
func (n *Node) ChildByFieldName(name string, _ *Language) *Node {
	return wrap(n.native.ChildByFieldName(name))
}
func (n *Node) FieldNameForChild(i int, _ *Language) string { return n.native.FieldNameForChild(i) }
func (n *Node) IsNamed() bool                               { return n.native.IsNamed() }
func (n *Node) IsExtra() bool                               { return n.native.IsExtra() }
func (n *Node) IsMissing() bool                             { return n.native.IsMissing() }
func (n *Node) IsError() bool                               { return n.native.IsError() }
func (n *Node) Symbol() uint16                              { return uint16(n.native.Symbol()) }
func (n *Node) Type(_ *Language) string                     { return n.native.Type() }
func (n *Node) Text(src []byte) string                      { return n.native.Content(src) }
func (n *Node) StartByte() uint32                           { return n.native.StartByte() }
func (n *Node) EndByte() uint32                             { return n.native.EndByte() }
func (n *Node) StartPoint() Point                           { return n.native.StartPoint() }
func (n *Node) EndPoint() Point                             { return n.native.EndPoint() }

type ParserPool struct{ lang *Language }
type ParserPoolOption func(*ParserPool)

func WithParserPoolTimeoutMicros(_ uint64) ParserPoolOption        { return func(*ParserPool) {} }
func NewParserPool(l *Language, _ ...ParserPoolOption) *ParserPool { return &ParserPool{lang: l} }

type Tree struct {
	native *ts.Tree
	parser *ts.Parser
}

func (t *Tree) RootNode() *Node { return wrap(t.native.RootNode()) }
func (t *Tree) Release()        { t.native.Close(); t.parser.Close() }
func (p *ParserPool) Parse(src []byte) (*Tree, error) {
	parser := ts.NewParser()
	parser.SetLanguage(p.lang.Native)
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		parser.Close()
		return nil, err
	}
	if tree == nil {
		parser.Close()
		return nil, fmt.Errorf("tree-sitter returned no tree")
	}
	return &Tree{native: tree, parser: parser}, nil
}
