package editor

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/Mojashi/memo/internal/live"
	"github.com/Mojashi/memo/internal/textbuf"
)

const indentUnit = "  "

// ── 選択とカーソル ─────────────────────────────────────

// Selection は前から順に並べた選択範囲。
func (e *Editor) Selection() (Pos, Pos) { return textbuf.Ordered(e.anchor, e.cursor) }

func (e *Editor) SelectedText() string {
	start, end := e.Selection()
	return e.buf.Slice(start, end)
}

func (e *Editor) Line(row int) string { return e.buf.Line(row) }

// Select は選択範囲を決める (anchor から cursor まで)。
func (e *Editor) Select(anchor, cursor Pos) {
	e.anchor, e.cursor = e.buf.Clamp(anchor), e.buf.Clamp(cursor)
	e.wantX = -1
	e.scrollIntoView()
}

// MoveTo はカーソルを動かす。extend なら選択を伸ばす。
func (e *Editor) MoveTo(p Pos, extend bool) {
	e.cursor = e.buf.Clamp(p)
	if !extend {
		e.anchor = e.cursor
	}
	e.wantX = -1
	e.scrollIntoView()
}

// JumpTo は行へ飛び、その行を画面の上の方に出す。
func (e *Editor) JumpTo(row int) {
	e.MoveTo(Pos{Row: row}, false)
	e.top = rowRef{line: max(0, row-3)}
}

// ── 編集 ───────────────────────────────────────────────

// Replace は [start, end) を text に置き換え、挿入の末尾を返す。カーソルは呼び出し側が決める。
func (e *Editor) Replace(start, end Pos, text string) Pos {
	after := e.buf.Replace(start, end, text, e.cursor)
	e.changed()
	return after
}

// Edit は fn の中の編集を1回の取り消し単位にまとめる。
func (e *Editor) Edit(fn func()) {
	e.buf.Begin("", e.cursor)
	fn()
	e.buf.Close()
	e.scrollIntoView()
}

func (e *Editor) changed() {
	if e.OnChange != nil {
		e.OnChange()
	}
}

// insert は選択範囲を text で置き換え、カーソルを挿入の後ろに置く。
// typing なら連続した入力を1回の取り消し単位にまとめる。
func (e *Editor) insert(text string, typing bool) {
	kind := ""
	if typing {
		kind = "type"
	}
	e.buf.Begin(kind, e.cursor)
	start, end := e.Selection()
	after := e.buf.ReplaceAt(start, end, text)
	e.anchor, e.cursor = after, after
	if !typing {
		e.buf.Close()
	}
	e.wantX = -1
	e.scrollIntoView()
	e.changed()
}

func (e *Editor) deleteRange(start, end Pos) {
	if start == end {
		return
	}
	e.buf.Begin("delete", e.cursor)
	e.buf.Replace(start, end, "", start)
	e.anchor, e.cursor = start, start
	e.wantX = -1
	e.scrollIntoView()
	e.changed()
}

func (e *Editor) deleteBackward(byWord bool) {
	if !e.selectionEmpty() {
		e.deleteRange(e.Selection())
		return
	}
	target := e.left(e.cursor, byWord)
	e.deleteRange(target, e.cursor)
}

func (e *Editor) deleteForward(byWord bool) {
	if !e.selectionEmpty() {
		e.deleteRange(e.Selection())
		return
	}
	e.deleteRange(e.cursor, e.right(e.cursor, byWord))
}

func (e *Editor) Undo() {
	if p, ok := e.buf.Undo(); ok {
		e.MoveTo(p, false)
		e.changed()
	}
}

func (e *Editor) Redo() {
	if p, ok := e.buf.Redo(); ok {
		e.MoveTo(p, false)
		e.changed()
	}
}

func (e *Editor) copySelection() {
	if !e.selectionEmpty() {
		e.clipboard.Copy(e.SelectedText())
	}
}

func (e *Editor) cut() {
	if e.selectionEmpty() {
		// 選択が無ければ行ごと切り取る
		row := e.cursor.Row
		e.Select(Pos{Row: row}, Pos{Row: row + 1})
		if row == e.lineCount()-1 {
			e.Select(Pos{Row: row}, Pos{Row: row, Col: e.lineLen(row)})
		}
	}
	e.copySelection()
	e.deleteRange(e.Selection())
}

func (e *Editor) paste(text string) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if text != "" {
		e.insert(text, false)
	}
}

// ── 行単位の操作 ───────────────────────────────────────

func (e *Editor) selectedRows() (int, int) {
	start, end := e.Selection()
	bottom := end.Row
	if bottom > start.Row && end.Col == 0 {
		bottom--
	}
	return start.Row, bottom
}

// indent は選択行 (またはカーソル行) の字下げを1段深くする。dedent なら浅くする。
func (e *Editor) indent(dedent bool) {
	top, bottom := e.selectedRows()
	shift := map[int]int{}
	e.Edit(func() {
		for row := top; row <= bottom; row++ {
			if !dedent {
				e.Replace(at(row, 0), at(row, 0), indentUnit)
				shift[row] = len(indentUnit)
				continue
			}
			line := e.Line(row)
			n := min(len(line)-len(strings.TrimLeft(line, " ")), len(indentUnit))
			if n > 0 {
				e.Replace(at(row, 0), at(row, n), "")
				shift[row] = -n
			}
		}
	})
	move := func(p Pos) Pos { return e.buf.Clamp(at(p.Row, max(0, p.Col+shift[p.Row]))) }
	e.anchor, e.cursor = move(e.anchor), move(e.cursor)
}

// tab は Tab キー。複数行の選択やリストの行なら字下げ、そうでなければ空白を入れる。
func (e *Editor) tab() {
	top, bottom := e.selectedRows()
	if top != bottom || (e.markdown && continuable.MatchString(e.Line(top))) {
		e.indent(false)
		return
	}
	e.insert(indentUnit, true)
}

var (
	continuable = regexp.MustCompile(`^(\s*)([-*+] \[[ xX]\] |[-*+] |(\d+)([.)]) |> ?)`)
	checkbox    = regexp.MustCompile(`^(\s*[-*+] \[)([ xX])(\])`)
)

// NextMarker は行のリスト記号から次の行の記号を返す。empty は本文が空か。
func NextMarker(line string) (marker string, empty, ok bool) {
	m := continuable.FindStringSubmatchIndex(line)
	if m == nil {
		return "", false, false
	}
	indent, mark := line[m[2]:m[3]], line[m[4]:m[5]]
	empty = strings.TrimSpace(line[m[1]:]) == ""
	switch {
	case m[6] >= 0:
		n, _ := strconv.Atoi(line[m[6]:m[7]])
		return indent + strconv.Itoa(n+1) + line[m[8]:m[9]] + " ", empty, true
	case strings.Contains(mark, "["):
		return indent + mark[:1] + " [ ] ", empty, true
	}
	return indent + mark, empty, true
}

// newline は Enter。リストなら次の項目の記号を続け、空の項目なら記号を消す。それ以外は字下げを保つ。
func (e *Editor) newline() {
	line := e.Line(e.cursor.Row)
	atEnd := e.selectionEmpty() && e.cursor.Col == e.lineLen(e.cursor.Row)
	if e.markdown && atEnd {
		if marker, empty, ok := NextMarker(line); ok {
			if empty {
				e.buf.Begin("", e.cursor)
				row := e.cursor.Row
				e.buf.Replace(at(row, 0), at(row, e.lineLen(row)), "", at(row, 0))
				e.anchor, e.cursor = at(row, 0), at(row, 0)
				e.buf.Close()
				e.changed()
				return
			}
			e.insert("\n"+marker, false)
			return
		}
	}
	indent := line[:len(line)-len(strings.TrimLeftFunc(line, unicode.IsSpace))]
	if e.markdown && !atEnd {
		indent = ""
	}
	e.insert("\n"+indent, false)
}

// ToggleCheckbox は row 行のチェックボックスを切り替える。
func (e *Editor) ToggleCheckbox(row int) bool {
	line := e.Line(row)
	m := checkbox.FindStringSubmatchIndex(line)
	if m == nil {
		return false
	}
	mark := "x"
	if line[m[4]:m[5]] != " " {
		mark = " "
	}
	col := len([]rune(line[:m[4]]))
	cursor, anchor := e.cursor, e.anchor
	e.Edit(func() { e.Replace(at(row, col), at(row, col+1), mark) })
	e.cursor, e.anchor = cursor, anchor
	return true
}

// checkboxAt は完成形で描いた row 行の x 桁目が ☐/☑ の上か。
func (e *Editor) checkboxAt(row, x int) bool {
	line := e.Line(row)
	if !checkbox.MatchString(line) {
		return false
	}
	indent := len([]rune(line)) - len([]rune(strings.TrimLeft(line, " \t")))
	l := e.layoutOf(row)
	_, markerX := live.XOf(l.units, l.breaks, indent)
	return x >= markerX && x <= markerX+1
}

// ── 単語・行の移動先 ───────────────────────────────────

// wordClass は単語の切れ目を決める文字の種類。漢字・ひらがな・カタカナ・英数字の境目で区切る。
func wordClass(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case unicode.Is(unicode.Han, r):
		return 2
	case unicode.Is(unicode.Hiragana, r):
		return 3
	case unicode.Is(unicode.Katakana, r) || r == 'ー':
		return 4
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return 5
	}
	return 1
}

func (e *Editor) left(p Pos, byWord bool) Pos {
	if p.Col == 0 {
		if p.Row == 0 {
			return p
		}
		return at(p.Row-1, e.lineLen(p.Row-1))
	}
	if !byWord {
		return at(p.Row, p.Col-1)
	}
	line := []rune(e.Line(p.Row))
	col := p.Col
	for col > 0 && wordClass(line[col-1]) == 0 {
		col--
	}
	if col > 0 {
		class := wordClass(line[col-1])
		for col > 0 && wordClass(line[col-1]) == class {
			col--
		}
	}
	return at(p.Row, col)
}

func (e *Editor) right(p Pos, byWord bool) Pos {
	n := e.lineLen(p.Row)
	if p.Col >= n {
		if p.Row+1 >= e.lineCount() {
			return p
		}
		return at(p.Row+1, 0)
	}
	if !byWord {
		return at(p.Row, p.Col+1)
	}
	line := []rune(e.Line(p.Row))
	col := p.Col
	for col < n && wordClass(line[col]) == 0 {
		col++
	}
	if col < n {
		class := wordClass(line[col])
		for col < n && wordClass(line[col]) == class {
			col++
		}
	}
	return at(p.Row, col)
}

// wordAt は p を含む単語の範囲 (ダブルクリックの選択)。
func (e *Editor) wordAt(p Pos) (Pos, Pos) {
	line := []rune(e.Line(p.Row))
	if len(line) == 0 {
		return p, p
	}
	col := min(p.Col, len(line)-1)
	class := wordClass(line[col])
	start, end := col, col
	for start > 0 && wordClass(line[start-1]) == class {
		start--
	}
	for end < len(line) && wordClass(line[end]) == class {
		end++
	}
	return at(p.Row, start), at(p.Row, end)
}

// home は行頭。字下げがあれば、まず本文の先頭へ (もう一度で本当の行頭へ)。
func (e *Editor) home(p Pos) Pos {
	line := []rune(e.Line(p.Row))
	first := 0
	for first < len(line) && unicode.IsSpace(line[first]) {
		first++
	}
	if p.Col == first {
		return at(p.Row, 0)
	}
	return at(p.Row, first)
}

// vertical は画面行で n 行上下に動いた位置。表示上の桁を保つ。
func (e *Editor) vertical(n int) Pos {
	r := e.rowOf(e.cursor)
	l := e.layoutOf(r.line)
	if e.wantX < 0 {
		_, e.wantX = live.XOf(l.units, l.breaks, e.cursor.Col)
	}
	for ; n > 0; n-- {
		next, ok := e.next(r)
		if !ok {
			return at(r.line, e.lineLen(r.line))
		}
		r = next
	}
	for ; n < 0; n++ {
		prev, ok := e.prev(r)
		if !ok {
			return at(0, 0)
		}
		r = prev
	}
	l = e.layoutOf(r.line)
	if r.section >= l.sections() {
		return at(l.figureEnd, 0) // 図の上に来たら、コードブロックの最後の行へ
	}
	return at(r.line, live.ColumnAt(l.units, l.breaks, r.section, e.wantX, e.lineLen(r.line)))
}

func (e *Editor) moveVertical(n int, extend bool) {
	want := e.wantX
	if want < 0 {
		r := e.rowOf(e.cursor)
		l := e.layoutOf(r.line)
		_, want = live.XOf(l.units, l.breaks, e.cursor.Col)
	}
	e.wantX = want
	p := e.vertical(n)
	e.cursor = p
	if !extend {
		e.anchor = p
	}
	e.wantX = want
	e.scrollIntoView()
}
