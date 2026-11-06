package ui

import (
	"github.com/gdamore/tcell/v2"

	"github.com/Mojashi/memo/internal/editor"
	"github.com/Mojashi/memo/internal/live"
	"github.com/Mojashi/memo/internal/theme"
)

// rect は画面上の長方形。
type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

func put(s tcell.Screen, x, y, maxWidth int, text string, style theme.Style) int {
	return editor.DrawText(s, x, y, maxWidth, text, style.Cell())
}

func fill(s tcell.Screen, r rect, style theme.Style) {
	editor.FillRect(s, r.x, r.y, r.w, r.h, style.Cell())
}

// segment は色付きの文字列の断片。
type segment struct {
	text  string
	style theme.Style
}

func putSegments(s tcell.Screen, x, y, maxWidth int, base theme.Style, segments ...segment) int {
	limit := x + maxWidth
	for _, seg := range segments {
		x = put(s, x, y, limit-x, seg.text, base.Over(seg.style))
	}
	return x
}

// lineInput は1行の文字入力欄。
type lineInput struct {
	text   []rune
	cursor int
	scroll int
}

func newLineInput(value string) *lineInput {
	r := []rune(value)
	return &lineInput{text: r, cursor: len(r)}
}

func (in *lineInput) value() string { return string(in.text) }

// handleKey は入力欄のキー操作。文字が変わったら changed。
func (in *lineInput) handleKey(ev *tcell.EventKey) (handled, changed bool) {
	switch ev.Key() {
	case tcell.KeyRune:
		if ev.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0 {
			return false, false
		}
		in.insert(string(ev.Rune()))
		return true, true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if in.cursor > 0 {
			in.text = append(in.text[:in.cursor-1], in.text[in.cursor:]...)
			in.cursor--
		}
		return true, true
	case tcell.KeyDelete:
		if in.cursor < len(in.text) {
			in.text = append(in.text[:in.cursor], in.text[in.cursor+1:]...)
		}
		return true, true
	case tcell.KeyLeft:
		in.cursor = max(0, in.cursor-1)
	case tcell.KeyRight:
		in.cursor = min(len(in.text), in.cursor+1)
	case tcell.KeyHome, tcell.KeyCtrlA:
		in.cursor = 0
	case tcell.KeyEnd, tcell.KeyCtrlE:
		in.cursor = len(in.text)
	case tcell.KeyCtrlU:
		in.text, in.cursor = nil, 0
		return true, true
	default:
		return false, false
	}
	return true, false
}

func (in *lineInput) insert(s string) {
	r := []rune(s)
	in.text = append(in.text[:in.cursor], append(r, in.text[in.cursor:]...)...)
	in.cursor += len(r)
}

// draw は入力欄を描き、カーソルの画面位置を返す。
func (in *lineInput) draw(s tcell.Screen, r rect, style theme.Style, placeholder string, placeholderStyle theme.Style) (int, int) {
	fill(s, r, style)
	if len(in.text) == 0 {
		put(s, r.x, r.y, r.w, placeholder, style.Over(placeholderStyle))
		return r.x, r.y
	}
	// カーソルが見えるように横にずらす
	in.scroll = min(in.scroll, in.cursor)
	for live.Width(string(in.text[in.scroll:in.cursor])) >= r.w {
		in.scroll++
	}
	put(s, r.x, r.y, r.w, string(in.text[in.scroll:]), style)
	return r.x + live.Width(string(in.text[in.scroll:in.cursor])), r.y
}
