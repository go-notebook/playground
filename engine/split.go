package engine

import (
	"go/parser"
	"go/scanner"
	"go/token"
	"strings"
)

// kind tells how a piece of a cell is handed to the interpreter.
type kind uint8

const (
	// stmts are statements, run in the notebook's top-level scope.
	stmts kind = iota
	// decls are declarations: imports, functions, types, constants and
	// variables. yaegi reads a source that starts with one as a file, which
	// may then hold nothing but declarations, so they go in pieces of their
	// own.
	decls
	// final is the cell's last statement when it is an expression: its
	// value is shown.
	final
)

// piece is a run of a cell's top-level statements of one kind.
type piece struct {
	kind kind
	src  string
	line int // 1-based line of the cell where src starts
}

// unit is one top-level statement or declaration of a cell.
type unit struct {
	kind       kind
	start, end int // byte offsets in the cell
	code       int // where its last token ends, before any comment
}

// tok is a scanned token at a byte offset.
type tok struct {
	off int
	tok token.Token
}

// split cuts a cell's source into pieces: runs of declarations, runs of
// statements, and the final expression. Each piece starts at the beginning
// of a line, so it keeps its indentation.
func split(src string) []piece {
	units := scan(src)
	if n := len(units); n > 0 && units[n-1].kind == stmts && isExpr(src[units[n-1].start:units[n-1].end]) {
		// Without the comment after it, so that it can be wrapped in a
		// call.
		units[n-1].kind = final
		units[n-1].end = units[n-1].code
	}
	var out []piece
	var starts []int
	for _, u := range units {
		if n := len(out); n > 0 && out[n-1].kind == u.kind && u.kind != final {
			out[n-1].src = src[starts[n-1]:u.end]
			continue
		}
		out = append(out, piece{kind: u.kind, src: src[u.start:u.end], line: 1 + strings.Count(src[:u.start], "\n")})
		starts = append(starts, u.start)
	}
	return out
}

// scan finds the top-level units of src: what lies between the semicolons,
// written or inserted at the end of a line, outside any bracket. A unit
// starts at the beginning of its first line, unless another one ends on
// that line. Scanning errors are ignored: the interpreter reports them.
func scan(src string) []unit {
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(file, []byte(src), nil, 0)

	var units []unit
	var cur []tok
	depth := 0
	code, prev := 0, 0
	flush := func(end int) {
		if len(cur) > 0 {
			// From the start of its line, or from the end of the unit
			// before it on the same line.
			start := max(prev, strings.LastIndexByte(src[:cur[0].off], '\n')+1)
			units = append(units, unit{kind: classify(cur), start: start, end: end, code: code})
			cur = cur[:0]
			prev = end
		}
	}
	for {
		pos, t, lit := s.Scan()
		off := file.Offset(pos)
		switch t {
		case token.EOF:
			flush(len(src))
			return units
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			depth--
		case token.SEMICOLON:
			if depth <= 0 {
				// A written semicolon ends the unit; one inserted at the
				// end of a line stands at the newline, or at the comment
				// before it, and the unit runs to the end of the line.
				end := off + 1
				if lit == "\n" {
					end = len(src)
					if i := strings.IndexByte(src[off:], '\n'); i >= 0 {
						end = off + i + 1
					}
				}
				flush(end)
				continue
			}
		}
		cur = append(cur, tok{off, t})
		n := len(lit) // a name, a literal or a keyword
		if n == 0 {
			n = len(t.String()) // an operator
		}
		code = off + n
	}
}

// classify tells a declaration from a statement by its first tokens. A
// "func" starts a declaration when a name follows it, or a receiver and then
// a name; otherwise it is a function literal in a statement, func() {...}().
func classify(ts []tok) kind {
	switch ts[0].tok {
	case token.IMPORT, token.TYPE, token.CONST, token.VAR:
		return decls
	case token.FUNC:
		if len(ts) > 1 && ts[1].tok == token.IDENT {
			return decls
		}
		// Skip the receiver, or the literal's parameters.
		depth := 0
		for i := 1; i < len(ts); i++ {
			switch ts[i].tok {
			case token.LPAREN:
				depth++
			case token.RPAREN:
				depth--
				if depth == 0 {
					if i+2 < len(ts) && ts[i+1].tok == token.IDENT && ts[i+2].tok == token.LPAREN {
						return decls
					}
					return stmts
				}
			}
		}
	}
	return stmts
}

// isExpr reports whether src is a single expression.
func isExpr(src string) bool {
	_, err := parser.ParseExpr(src)
	return err == nil
}
