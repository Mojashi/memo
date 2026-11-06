// Package editor は Markdown を書きやすくしたテキストエディタ。
//
//   - ライブプレビュー: カーソル (選択) のある行だけ Markdown の記号を見せ、他の行は完成形で描く。
//     エディタからフォーカスが外れると全行を完成形で見せる
//   - Markdown 以外のファイルは行番号つきで、言語に応じて色付けする
//   - 折り返しは表示幅で決め、位置は元テキストの (行, 列) のまま扱う
package editor

import (
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"

	"github.com/Mojashi/memo/internal/format"
	"github.com/Mojashi/memo/internal/live"
	"github.com/Mojashi/memo/internal/syntax"
	"github.com/Mojashi/memo/internal/textbuf"
	"github.com/Mojashi/memo/internal/theme"
)

type Pos = textbuf.Pos

func at(row, col int) Pos { return Pos{Row: row, Col: col} }

// Clipboard はシステムのクリップボード。
type Clipboard interface {
	Copy(text string)
	Paste() string
}

// rowRef は画面の1行 = ある行の折り返し section 段目。
type rowRef struct{ line, section int }

type layoutKey struct {
	text      string
	block     string
	row       int // コードの色は行ごとに違うので、コードファイルでは行番号も鍵にする
	width     int
	concealed bool
	diagram   string // Mermaid のコードブロックなら、その図のソース
}

type layout struct {
	units  []live.Unit
	breaks []int
	fill   theme.Style
	// 図: Mermaid のコードブロックは、開始行の下に図を描き、残りの行は画面に出さない
	figure       []string
	hidden       bool
	figureSource int // 図をクリックしたときに移る行 (ソースの1行目)
	figureEnd    int // 下から図に入ったときに移る行 (コードブロックの最終行)
}

func (l *layout) sections() int { return len(l.breaks) + 1 }

// rows はこの行が画面で使う行数 (隠れた行は 0)。
func (l *layout) rows() int {
	if l.hidden {
		return 0
	}
	return l.sections() + len(l.figure)
}

type Editor struct {
	buf            *textbuf.Buffer
	anchor, cursor Pos
	wantX          int // 上下移動で保つ表示上の桁。-1 なら未設定

	markdown bool
	live     bool
	focused  bool
	lexer    chroma.Lexer

	theme    theme.Theme
	palette  *live.Palette
	colors   syntax.Colors
	code     [][]theme.Style // コードファイルの色 (codeText の時点のもの)
	codeText []string

	blocks        []live.Block
	blocksVersion int
	layouts       map[layoutKey]*layout

	// 画面上の配置 (Draw で決まる)
	x, y, w, h   int
	textX, textW int
	gutterW      int
	top          rowRef

	clipboard Clipboard
	pasting   bool
	pasted    strings.Builder
	mouse     mouseState

	figures figureCache

	// OnChange は本文が変わったときに呼ばれる。
	OnChange func()
	// Post は別スレッドで終わった処理 (図の描画) を画面の更新に渡す。
	Post func(func())
}

func New(clipboard Clipboard, t theme.Theme) *Editor {
	e := &Editor{buf: textbuf.New(""), wantX: -1, live: true, clipboard: clipboard, blocksVersion: -1}
	e.SetTheme(t)
	return e
}

// Load は新しい文書を開く。markdown ならライブプレビュー、そうでなければ lexer で色付けする。
func (e *Editor) Load(text string, markdown bool, lexer chroma.Lexer) {
	e.buf = textbuf.New(text)
	e.markdown = markdown
	e.lexer = lexer
	e.anchor, e.cursor = Pos{}, Pos{}
	e.wantX = -1
	e.top = rowRef{}
	e.blocksVersion = -1
	e.code, e.codeText = nil, nil
	e.Recolor()
	e.invalidate()
}

func (e *Editor) Text() string         { return e.buf.Text() }
func (e *Editor) Lines() []string      { return e.buf.Lines() }
func (e *Editor) Cursor() Pos          { return e.cursor }
func (e *Editor) IsMarkdown() bool     { return e.markdown }
func (e *Editor) SetFocused(f bool)    { e.focused = f }
func (e *Editor) Focused() bool        { return e.focused }
func (e *Editor) SetLive(live bool)    { e.live = live }
func (e *Editor) Live() bool           { return e.live }
func (e *Editor) invalidate()          { e.layouts = map[layoutKey]*layout{} }
func (e *Editor) lineCount() int       { return e.buf.LineCount() }
func (e *Editor) lineLen(i int) int    { return e.buf.LineLen(i) }
func (e *Editor) selectionEmpty() bool { return e.anchor == e.cursor }

func (e *Editor) SetTheme(t theme.Theme) {
	e.theme = t
	e.colors = syntax.NewColors(t)
	e.palette = PaletteFrom(t)
	e.palette.CodeColors = e.colors.FenceColors()
	if e.code != nil {
		e.Recolor()
	}
	e.invalidate()
}

// Recolor はコードファイルの色付けをやり直す (入力が落ち着いたときに呼ぶ)。
func (e *Editor) Recolor() {
	if e.markdown || e.lexer == nil {
		return
	}
	lines := e.buf.Lines()
	e.code = e.colors.Lines(e.lexer, e.buf.Text())
	e.codeText = append(e.codeText[:0], lines...)
	e.invalidate()
}

// PaletteFrom はアプリのテーマ色からライブプレビューの配色を作る。
func PaletteFrom(t theme.Theme) *live.Palette {
	muted := t.Muted()
	codeBg := t.CodeBackground()
	return &live.Palette{
		Heading: [6]theme.Style{
			{Fg: t.Primary, Bold: true, Underline: true},
			{Fg: t.Accent, Bold: true},
			{Fg: t.Secondary, Bold: true},
			{Fg: t.Secondary, Bold: true, Italic: true},
			{Fg: muted, Bold: true},
			{Fg: muted, Bold: true, Italic: true},
		},
		Marker:      theme.Style{Fg: muted},
		Bullet:      theme.Style{Fg: t.Accent, Bold: true},
		TaskOpen:    theme.Style{Fg: t.Accent, Bold: true},
		TaskDone:    theme.Style{Fg: t.Success, Bold: true},
		DoneText:    theme.Style{Fg: muted, Strike: true},
		QuoteBar:    theme.Style{Fg: t.Accent},
		QuoteText:   theme.Style{Fg: muted, Italic: true},
		Code:        theme.Style{Fg: t.Warning, Bg: codeBg},
		CodeBlock:   theme.Style{Bg: codeBg},
		CodeLabel:   theme.Style{Fg: muted, Bg: codeBg, Italic: true},
		Link:        theme.Style{Fg: t.Primary, Underline: true},
		Rule:        theme.Style{Fg: muted},
		TableBorder: theme.Style{Fg: muted},
		TableHeader: theme.Style{Fg: t.Accent, Bold: true},
		Bold:        theme.Style{Bold: true},
		Italic:      theme.Style{Italic: true},
		Strike:      theme.Style{Strike: true},
	}
}

// ── 行の見え方 ──────────────────────────────────────────

func (e *Editor) blockOf(i int) live.Block {
	if !e.markdown {
		return live.Block{}
	}
	if e.blocksVersion != e.buf.Version {
		e.blocks = live.Analyze(e.buf.Lines())
		e.blocksVersion = e.buf.Version
	}
	return e.blocks[i]
}

func blockKey(b live.Block) string {
	var s strings.Builder
	s.WriteString(strconv.Itoa(int(b.Kind)))
	s.WriteString(b.Language)
	if b.Header {
		s.WriteByte('H')
	}
	if b.Delimiter {
		s.WriteByte('D')
	}
	for _, w := range b.Widths {
		s.WriteByte(',')
		s.WriteString(strconv.Itoa(w))
	}
	return s.String()
}

// concealed は i 行目を記号を隠した完成形で描くか。
func (e *Editor) concealed(i int) bool {
	if !e.markdown || !e.live {
		return false
	}
	if !e.focused {
		return true
	}
	top, bottom := textbuf.Ordered(e.anchor, e.cursor)
	// 図はコードブロックのどこかにカーソルがあれば、ブロックごとソースを見せる
	if block := e.blockOf(i); block.IsDiagram() {
		return block.Close < top.Row || block.Open > bottom.Row
	}
	return i < top.Row || i > bottom.Row
}

func (e *Editor) layoutOf(i int) *layout {
	text := e.buf.Line(i)
	block := e.blockOf(i)
	key := layoutKey{text: text, block: blockKey(block), width: e.textW, concealed: e.concealed(i), row: -1}
	if !e.markdown {
		key.row = i
	}
	if key.concealed && block.IsDiagram() {
		key.diagram = e.diagramSource(block)
	}
	if l, ok := e.layouts[key]; ok {
		return l
	}
	if len(e.layouts) > 4*e.lineCount()+1024 {
		e.invalidate()
	}
	if key.diagram != "" {
		if l := e.figureLayout(i, text, block, key); l != nil {
			e.layouts[key] = l
			return l
		}
	}
	var view live.View
	if e.markdown {
		view = live.Render(text, block, e.textW, key.concealed, e.palette)
	} else {
		view = e.codeView(i, text)
	}
	l := &layout{units: view.Units, fill: view.Fill}
	// 完成形の表は折り返すと崩れるので、収まらない分は切る
	if !(key.concealed && block.Kind == live.TableRow) {
		l.breaks = live.Wrap(view.Units, e.textW)
	}
	e.layouts[key] = l
	return l
}

// codeView はコードファイルの1行。色付けが今の行と食い違っていれば色なしで描く。
func (e *Editor) codeView(i int, text string) live.View {
	var styles []theme.Style
	if i < len(e.codeText) && e.codeText[i] == text && i < len(e.code) {
		styles = e.code[i]
	}
	view := live.Render(text, live.Block{Kind: live.FenceBody}, e.textW, false, &live.Palette{})
	for k := range view.Units {
		if k < len(styles) {
			view.Units[k].Style = styles[k]
		}
	}
	return view
}

// ── 画面の行と文書の行の対応 ─────────────────────────────

func (e *Editor) rowOf(p Pos) rowRef {
	l := e.layoutOf(p.Row)
	return rowRef{p.Row, live.SectionOf(l.breaks, p.Col)}
}

// next は1つ下の画面行。最後なら ok=false。
func (e *Editor) next(r rowRef) (rowRef, bool) {
	if r.section+1 < e.layoutOf(r.line).rows() {
		return rowRef{r.line, r.section + 1}, true
	}
	for line := r.line + 1; line < e.lineCount(); line++ {
		if e.layoutOf(line).rows() > 0 {
			return rowRef{line, 0}, true
		}
	}
	return r, false
}

// prev は1つ上の画面行。先頭なら ok=false。
func (e *Editor) prev(r rowRef) (rowRef, bool) {
	if r.section > 0 {
		return rowRef{r.line, r.section - 1}, true
	}
	for line := r.line - 1; line >= 0; line-- {
		if rows := e.layoutOf(line).rows(); rows > 0 {
			return rowRef{line, rows - 1}, true
		}
	}
	return r, false
}

func before(a, b rowRef) bool {
	return a.line < b.line || (a.line == b.line && a.section < b.section)
}

// clampTop は top を今の折り返しで有効な位置に直す。
func (e *Editor) clampTop() {
	e.top.line = max(0, min(e.top.line, e.lineCount()-1))
	for e.top.line > 0 && e.layoutOf(e.top.line).hidden {
		e.top = rowRef{e.top.line - 1, 1 << 30}
	}
	e.top.section = max(0, min(e.top.section, e.layoutOf(e.top.line).rows()-1))
}

// scrollIntoView はカーソルが画面に入るようにスクロールする。
func (e *Editor) scrollIntoView() {
	if e.h <= 0 {
		return
	}
	e.clampTop()
	c := e.rowOf(e.cursor)
	if before(c, e.top) {
		e.top = c
		return
	}
	r, distance := e.top, 0
	for before(r, c) {
		r, _ = e.next(r)
		distance++
		if distance >= e.h {
			break
		}
	}
	if distance < e.h {
		return
	}
	top := c
	for range e.h - 1 {
		top, _ = e.prev(top)
	}
	e.top = top
}

// Scroll は画面を n 行だけ動かす (カーソルは動かさない)。
func (e *Editor) Scroll(n int) {
	e.clampTop()
	for ; n > 0; n-- {
		r, ok := e.next(e.top)
		if !ok {
			break
		}
		e.top = r
	}
	for ; n < 0; n++ {
		r, ok := e.prev(e.top)
		if !ok {
			break
		}
		e.top = r
	}
}

// posAt は画面上の (x, y) (エディタ内の相対位置) に対応する文書の位置。
func (e *Editor) posAt(x, y int) Pos {
	e.clampTop()
	r := e.top
	for range y {
		next, ok := e.next(r)
		if !ok {
			return at(r.line, e.lineLen(r.line))
		}
		r = next
	}
	l := e.layoutOf(r.line)
	if r.section >= l.sections() {
		return at(l.figureSource, 0) // 図をクリックしたらソースを開く
	}
	return at(r.line, live.ColumnAt(l.units, l.breaks, r.section, max(0, x-(e.textX-e.x)), e.lineLen(r.line)))
}

// ── 描画 ────────────────────────────────────────────────

// Draw は (x, y, w, h) の範囲に描く。カーソルを出す位置を返す (見えなければ ok=false)。
func (e *Editor) Draw(screen tcell.Screen, x, y, w, h int) (cx, cy int, ok bool) {
	e.x, e.y, e.w, e.h = x, y, w, h
	e.gutterW = 0
	padding := 2
	if !e.markdown {
		e.gutterW = len(strconv.Itoa(e.lineCount())) + 2
		padding = 0
	}
	e.textX = x + e.gutterW + padding
	if width := max(1, w-e.gutterW-2*padding); width != e.textW {
		e.textW = width
	}

	base := e.theme.Text()
	fillRect(screen, x, y, w, h, base.Cell())
	e.clampTop()

	start, end := textbuf.Ordered(e.anchor, e.cursor)
	cursorRow := e.rowOf(e.cursor)
	r, more := e.top, true
	for row := 0; row < h && more; row++ {
		l := e.layoutOf(r.line)
		sy := y + row
		if e.gutterW > 0 {
			number := ""
			if r.section == 0 {
				number = strconv.Itoa(r.line + 1)
			}
			numberStyle := theme.Style{Fg: e.theme.Muted(), Bg: e.theme.Background}
			if r.line == e.cursor.Row {
				numberStyle.Fg = e.theme.Foreground
			}
			drawText(screen, x, sy, e.gutterW, strings.Repeat(" ", e.gutterW-2-len(number))+number+"  ", numberStyle.Cell())
		}
		if l.fill != (theme.Style{}) {
			fillRect(screen, e.textX, sy, e.textW, 1, base.Over(l.fill).Cell())
		}
		if r.section >= l.sections() {
			drawText(screen, e.textX+1, sy, e.textW-1, l.figure[r.section-l.sections()], base.Over(l.fill).Cell())
			r, more = e.next(r)
			continue
		}
		sx := e.textX
		for _, u := range live.SectionUnits(l.units, l.breaks, r.section) {
			style := base.Over(l.fill).Over(u.Style)
			if !e.selectionEmpty() && inSelection(u, r.line, start, end) {
				style.Bg = e.theme.Selection()
			}
			sx = drawText(screen, sx, sy, e.textX+e.textW-sx, u.Text, style.Cell())
		}
		// 行末の改行も選択に含まれていれば、1マス塗って見せる
		if !e.selectionEmpty() && r.section == l.sections()-1 && r.line >= start.Row && r.line < end.Row && sx < e.textX+e.textW {
			screen.SetContent(sx, sy, ' ', nil, base.Over(theme.Style{Bg: e.theme.Selection()}).Cell())
		}
		if r == cursorRow {
			_, cx = live.XOf(l.units, l.breaks, e.cursor.Col)
			cx, cy, ok = min(e.textX+cx, x+w-1), sy, true
		}
		r, more = e.next(r)
	}
	return cx, cy, ok
}

func inSelection(u live.Unit, line int, start, end Pos) bool {
	p := Pos{Row: line, Col: u.RawStart}
	return !p.Before(start) && p.Before(end) && u.RawEnd > u.RawStart
}

func fillRect(screen tcell.Screen, x, y, w, h int, style tcell.Style) {
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			screen.SetContent(col, row, ' ', nil, style)
		}
	}
}

// drawText は1行に文字列を描き、次に描く x を返す。maxWidth を超える分は描かない。
func drawText(screen tcell.Screen, x, y, maxWidth int, text string, style tcell.Style) int {
	limit := x + maxWidth
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		cluster := g.Str()
		w := live.Width(cluster)
		if x+w > limit {
			break
		}
		runes := g.Runes()
		screen.SetContent(x, y, runes[0], runes[1:], style)
		x += w
	}
	return x
}

// DrawText は他の部品からも使う文字列描画。
func DrawText(screen tcell.Screen, x, y, maxWidth int, text string, style tcell.Style) int {
	return drawText(screen, x, y, maxWidth, text, style)
}

// FillRect は他の部品からも使う塗りつぶし。
func FillRect(screen tcell.Screen, x, y, w, h int, style tcell.Style) {
	fillRect(screen, x, y, w, h, style)
}

// ApplyFormat は書式 (太字・見出しなど) を当てる。1回の取り消しで戻る。
func (e *Editor) ApplyFormat(f format.Format) {
	e.Edit(func() { f.Apply(e) })
}

// Version は編集のたびに変わる番号 (変更の検出用)。
func (e *Editor) Version() int { return e.buf.Version }

// End は文書の末尾。
func (e *Editor) End() Pos { return e.buf.End() }
