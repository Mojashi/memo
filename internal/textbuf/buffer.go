// Package textbuf は行単位のテキストと、編集の取り消し・やり直し。
//
// 位置は (行, 列) で、列は rune 単位。
package textbuf

import (
	"slices"
	"strings"
	"unicode/utf8"
)

type Pos struct{ Row, Col int }

func (p Pos) Before(q Pos) bool { return p.Row < q.Row || (p.Row == q.Row && p.Col < q.Col) }

// Ordered は2つの位置を前から順に並べる。
func Ordered(a, b Pos) (Pos, Pos) {
	if b.Before(a) {
		return b, a
	}
	return a, b
}

// change は1回の置き換え。逆向きに当てれば元に戻る。
type change struct {
	start    Pos
	removed  string
	inserted string
}

// Group は「1回の取り消しで戻る」編集のまとまり。前後のカーソル位置を覚えておく。
type Group struct {
	changes      []change
	CursorBefore Pos
	CursorAfter  Pos
	kind         string
}

type Buffer struct {
	lines   []string
	undo    []*Group
	redo    []*Group
	open    *Group // まだ閉じていないまとまり
	Version int    // 編集のたびに増える
}

func New(text string) *Buffer {
	return &Buffer{lines: strings.Split(text, "\n")}
}

func (b *Buffer) Lines() []string     { return b.lines }
func (b *Buffer) LineCount() int      { return len(b.lines) }
func (b *Buffer) Line(row int) string { return b.lines[row] }
func (b *Buffer) Text() string        { return strings.Join(b.lines, "\n") }
func (b *Buffer) LineLen(row int) int { return utf8.RuneCountInString(b.lines[row]) }

func (b *Buffer) End() Pos {
	last := len(b.lines) - 1
	return Pos{last, b.LineLen(last)}
}

// Clamp は位置を文書の中に収める。
func (b *Buffer) Clamp(p Pos) Pos {
	p.Row = max(0, min(p.Row, len(b.lines)-1))
	p.Col = max(0, min(p.Col, b.LineLen(p.Row)))
	return p
}

// Slice は [start, end) のテキスト。
func (b *Buffer) Slice(start, end Pos) string {
	start, end = Ordered(start, end)
	if start.Row == end.Row {
		r := []rune(b.lines[start.Row])
		return string(r[start.Col:end.Col])
	}
	var s strings.Builder
	s.WriteString(string([]rune(b.lines[start.Row])[start.Col:]))
	for row := start.Row + 1; row < end.Row; row++ {
		s.WriteByte('\n')
		s.WriteString(b.lines[row])
	}
	s.WriteByte('\n')
	s.WriteString(string([]rune(b.lines[end.Row])[:end.Col]))
	return s.String()
}

// Begin は新しい取り消し単位を始める。kind が前回と同じなら (連続した入力など) 前のまとまりに足す。
func (b *Buffer) Begin(kind string, cursor Pos) {
	if b.open != nil && kind != "" && b.open.kind == kind {
		return
	}
	b.Close()
	b.open = &Group{CursorBefore: cursor, kind: kind}
}

// Close は今のまとまりを閉じる。次の編集は別の取り消し単位になる。
func (b *Buffer) Close() {
	if b.open != nil && len(b.open.changes) > 0 {
		b.undo = append(b.undo, b.open)
	}
	b.open = nil
}

// Replace は [start, end) を text に置き換え、挿入したテキストの末尾の位置を返す。
func (b *Buffer) Replace(start, end Pos, text string, cursorAfter Pos) Pos {
	start, end = Ordered(b.Clamp(start), b.Clamp(end))
	if b.open == nil {
		b.Begin("", start)
	}
	removed, after := b.splice(start, end, text)
	b.open.changes = append(b.open.changes, change{start, removed, text})
	b.open.CursorAfter = cursorAfter
	b.redo = nil
	return after
}

// ReplaceAt は Replace と同じだが、編集後のカーソルを挿入の末尾にする。
func (b *Buffer) ReplaceAt(start, end Pos, text string) Pos {
	start, end = Ordered(b.Clamp(start), b.Clamp(end))
	after := endOf(start, text)
	return b.Replace(start, end, text, after)
}

func endOf(start Pos, text string) Pos {
	parts := strings.Split(text, "\n")
	last := len(parts) - 1
	if last == 0 {
		return Pos{start.Row, start.Col + utf8.RuneCountInString(text)}
	}
	return Pos{start.Row + last, utf8.RuneCountInString(parts[last])}
}

func (b *Buffer) splice(start, end Pos, text string) (removed string, after Pos) {
	removed = b.Slice(start, end)
	first := []rune(b.lines[start.Row])
	last := []rune(b.lines[end.Row])
	prefix, suffix := string(first[:start.Col]), string(last[end.Col:])
	parts := strings.Split(text, "\n")
	n := len(parts) - 1
	after = Pos{start.Row + n, utf8.RuneCountInString(parts[n])}
	if n == 0 {
		after.Col += start.Col
	}
	parts[0] = prefix + parts[0]
	parts[n] += suffix
	b.lines = slices.Replace(b.lines, start.Row, end.Row+1, parts...)
	b.Version++
	return removed, after
}

// Undo は最後のまとまりを取り消し、戻したあとのカーソル位置を返す。
func (b *Buffer) Undo() (Pos, bool) {
	b.Close()
	if len(b.undo) == 0 {
		return Pos{}, false
	}
	g := b.undo[len(b.undo)-1]
	b.undo = b.undo[:len(b.undo)-1]
	for i := len(g.changes) - 1; i >= 0; i-- {
		c := g.changes[i]
		b.splice(c.start, endOf(c.start, c.inserted), c.removed)
	}
	b.redo = append(b.redo, g)
	return g.CursorBefore, true
}

// Redo は取り消したまとまりをやり直す。
func (b *Buffer) Redo() (Pos, bool) {
	b.Close()
	if len(b.redo) == 0 {
		return Pos{}, false
	}
	g := b.redo[len(b.redo)-1]
	b.redo = b.redo[:len(b.redo)-1]
	for _, c := range g.changes {
		b.splice(c.start, endOf(c.start, c.removed), c.inserted)
	}
	b.undo = append(b.undo, g)
	return g.CursorAfter, true
}
