package ui

import (
	"github.com/gdamore/tcell/v2"

	"github.com/Mojashi/memo/internal/live"
	"github.com/Mojashi/memo/internal/theme"
)

// dialog は画面の上に重ねる入力欄・確認・一覧。キー入力を独占する。
type dialog interface {
	draw(s tcell.Screen, screen rect, t theme.Theme) (cursorX, cursorY int, cursor bool)
	handleKey(ev *tcell.EventKey) (closed bool)
	handleMouse(ev *tcell.EventMouse) (closed bool)
	paste(text string)
}

func dialogFrame(s tcell.Screen, screen rect, t theme.Theme, width, height int, title string) rect {
	w := min(width, screen.w-4)
	h := min(height, screen.h-2)
	r := rect{screen.x + (screen.w-w)/2, screen.y + max(1, (screen.h-h)/3), w, h}
	fill(s, r, theme.Style{Fg: t.Foreground, Bg: t.Surface})
	border := theme.Style{Fg: t.Accent, Bg: t.Surface}
	put(s, r.x, r.y, 1, "╭", border)
	put(s, r.x+r.w-1, r.y, 1, "╮", border)
	put(s, r.x, r.y+r.h-1, 1, "╰", border)
	put(s, r.x+r.w-1, r.y+r.h-1, 1, "╯", border)
	for x := r.x + 1; x < r.x+r.w-1; x++ {
		put(s, x, r.y, 1, "─", border)
		put(s, x, r.y+r.h-1, 1, "─", border)
	}
	for y := r.y + 1; y < r.y+r.h-1; y++ {
		put(s, r.x, y, 1, "│", border)
		put(s, r.x+r.w-1, y, 1, "│", border)
	}
	put(s, r.x+2, r.y, r.w-4, " "+title+" ", theme.Style{Fg: t.Foreground, Bg: t.Surface, Bold: true})
	return rect{r.x + 2, r.y + 1, r.w - 4, r.h - 2}
}

// ── 名前の入力 ─────────────────────────────────────────

type promptDialog struct {
	title, placeholder string
	input              *lineInput
	done               func(value string, ok bool)
}

func newPrompt(title, placeholder, value string, done func(string, bool)) *promptDialog {
	return &promptDialog{title: title, placeholder: placeholder, input: newLineInput(value), done: done}
}

func (d *promptDialog) draw(s tcell.Screen, screen rect, t theme.Theme) (int, int, bool) {
	inner := dialogFrame(s, screen, t, 64, 6, d.title)
	x, y := d.input.draw(s, rect{inner.x, inner.y + 1, inner.w, 1}, theme.Style{Fg: t.Foreground, Bg: t.Panel}, d.placeholder, t.Faint())
	put(s, inner.x, inner.y+3, inner.w, "Enter で決定 / Esc でキャンセル", theme.Style{Fg: t.Muted(), Bg: t.Surface})
	return x, y, true
}

func (d *promptDialog) handleKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEsc:
		d.done("", false)
		return true
	case tcell.KeyEnter:
		value := d.input.value()
		d.done(value, value != "")
		return true
	}
	d.input.handleKey(ev)
	return false
}

func (d *promptDialog) handleMouse(*tcell.EventMouse) bool { return false }
func (d *promptDialog) paste(text string)                  { d.input.insert(text) }

// ── 確認 ───────────────────────────────────────────────

type confirmDialog struct {
	message, confirm string
	done             func(ok bool)
}

func (d *confirmDialog) draw(s tcell.Screen, screen rect, t theme.Theme) (int, int, bool) {
	inner := dialogFrame(s, screen, t, max(48, live.Width(d.message)+6), 6, "確認")
	put(s, inner.x, inner.y+1, inner.w, d.message, theme.Style{Fg: t.Foreground, Bg: t.Surface})
	hint := " Enter: " + d.confirm + "   Esc: キャンセル "
	put(s, inner.x, inner.y+3, inner.w, hint, theme.Style{Fg: t.Error, Bg: t.Surface, Bold: true})
	return 0, 0, false
}

func (d *confirmDialog) handleKey(ev *tcell.EventKey) bool {
	switch {
	case ev.Key() == tcell.KeyEnter || (ev.Key() == tcell.KeyRune && (ev.Rune() == 'y' || ev.Rune() == 'Y')):
		d.done(true)
		return true
	case ev.Key() == tcell.KeyEsc || (ev.Key() == tcell.KeyRune && (ev.Rune() == 'n' || ev.Rune() == 'N')):
		d.done(false)
		return true
	}
	return false
}

func (d *confirmDialog) handleMouse(*tcell.EventMouse) bool { return false }
func (d *confirmDialog) paste(string)                       {}

// ── 一覧から選ぶ (検索・ファイルを開く・コマンド) ──────────

// pickItem は一覧の1件。
type pickItem struct {
	label  []segment
	detail string
	choose func()
}

// pickerDialog は入力に応じて一覧を絞り込み、選んだものを実行する。
type pickerDialog struct {
	title, placeholder string
	input              *lineInput
	items              []pickItem
	selected, scroll   int
	refresh            func(query string) // 入力が変わったら呼ぶ。結果は setItems で渡す
	listArea           rect
	width              int
}

func newPicker(title, placeholder string, width int, refresh func(query string)) *pickerDialog {
	return &pickerDialog{title: title, placeholder: placeholder, input: newLineInput(""), refresh: refresh, width: width}
}

func (d *pickerDialog) setItems(items []pickItem) {
	d.items = items
	d.selected, d.scroll = 0, 0
}

func (d *pickerDialog) draw(s tcell.Screen, screen rect, t theme.Theme) (int, int, bool) {
	inner := dialogFrame(s, screen, t, d.width, screen.h*4/5, d.title)
	cx, cy := d.input.draw(s, rect{inner.x, inner.y, inner.w, 1}, theme.Style{Fg: t.Foreground, Bg: t.Panel}, d.placeholder, t.Faint())
	d.listArea = rect{inner.x, inner.y + 2, inner.w, inner.h - 2}
	rows := d.listArea.h
	if d.selected < d.scroll {
		d.scroll = d.selected
	}
	if d.selected >= d.scroll+rows {
		d.scroll = d.selected - rows + 1
	}
	if len(d.items) == 0 && d.input.value() != "" {
		put(s, d.listArea.x, d.listArea.y, d.listArea.w, "見つかりません", theme.Style{Fg: t.Muted(), Bg: t.Surface})
	}
	for row := 0; row < rows && d.scroll+row < len(d.items); row++ {
		index := d.scroll + row
		item := d.items[index]
		base := theme.Style{Fg: t.Foreground, Bg: t.Surface}
		if index == d.selected {
			base.Bg = t.Selection()
		}
		y := d.listArea.y + row
		fill(s, rect{d.listArea.x, y, d.listArea.w, 1}, base)
		x := putSegments(s, d.listArea.x+1, y, d.listArea.w-2, base, item.label...)
		if item.detail != "" {
			put(s, x+2, y, d.listArea.x+d.listArea.w-x-3, item.detail, base.Over(t.Faint()))
		}
	}
	return cx, cy, true
}

func (d *pickerDialog) handleKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEsc:
		return true
	case tcell.KeyEnter:
		if d.selected < len(d.items) {
			d.items[d.selected].choose()
			return true
		}
		return false
	case tcell.KeyUp, tcell.KeyCtrlP:
		d.selected = max(0, d.selected-1)
		return false
	case tcell.KeyDown, tcell.KeyCtrlN:
		d.selected = min(max(0, len(d.items)-1), d.selected+1)
		return false
	case tcell.KeyPgUp:
		d.selected = max(0, d.selected-d.listArea.h)
		return false
	case tcell.KeyPgDn:
		d.selected = min(max(0, len(d.items)-1), d.selected+d.listArea.h)
		return false
	}
	if _, changed := d.input.handleKey(ev); changed {
		d.refresh(d.input.value())
	}
	return false
}

func (d *pickerDialog) handleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	switch {
	case ev.Buttons()&tcell.WheelUp != 0:
		d.selected = max(0, d.selected-3)
	case ev.Buttons()&tcell.WheelDown != 0:
		d.selected = min(max(0, len(d.items)-1), d.selected+3)
	case ev.Buttons()&tcell.Button1 != 0 && d.listArea.contains(x, y):
		index := d.scroll + y - d.listArea.y
		if index < len(d.items) {
			d.items[index].choose()
			return true
		}
	}
	return false
}

func (d *pickerDialog) paste(text string) {
	d.input.insert(text)
	d.refresh(d.input.value())
}
