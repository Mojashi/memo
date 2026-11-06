package editor

import (
	"time"
	"unicode"

	"github.com/gdamore/tcell/v2"
)

const (
	doubleClick = 400 * time.Millisecond
	wheelRows   = 3
)

type mouseState struct {
	pressed   bool
	lastClick time.Time
	lastPos   Pos
}

// IsCtrl は Ctrl+letter か。端末によって KeyCtrlX でも Rune+ModCtrl でも届く。
func IsCtrl(ev *tcell.EventKey, letter rune) bool {
	if ev.Key() == tcell.KeyCtrlA+tcell.Key(letter-'a') {
		return true
	}
	return ev.Key() == tcell.KeyRune && ev.Modifiers()&tcell.ModCtrl != 0 && unicode.ToLower(ev.Rune()) == letter
}

// PasteStart / PasteEnd は括弧付きペースト。間に届いたキーはそのまま文字として貼る。
func (e *Editor) PasteStart() {
	e.pasting = true
	e.pasted.Reset()
}

func (e *Editor) PasteEnd() {
	e.pasting = false
	e.paste(e.pasted.String())
}

// HandleKey はキー入力を処理する。処理したら true。
func (e *Editor) HandleKey(ev *tcell.EventKey) bool {
	if e.pasting {
		switch ev.Key() {
		case tcell.KeyRune:
			e.pasted.WriteRune(ev.Rune())
		case tcell.KeyEnter, tcell.KeyCtrlJ:
			e.pasted.WriteByte('\n')
		case tcell.KeyTab:
			e.pasted.WriteByte('\t')
		}
		return true
	}

	mods := ev.Modifiers()
	shift := mods&tcell.ModShift != 0
	word := mods&(tcell.ModAlt|tcell.ModCtrl) != 0 // Option+矢印 / Ctrl+矢印 で単語ごと

	if action := e.shortcut(ev); action != nil {
		action()
		return true
	}
	if isCtrlCombo(ev) {
		return false // アプリのショートカット
	}

	switch ev.Key() {
	case tcell.KeyRune:
		// Option+b / Option+f (emacs 流の単語移動を送る端末向け)
		if mods&tcell.ModAlt != 0 {
			switch ev.Rune() {
			case 'b':
				e.MoveTo(e.left(e.cursor, true), shift)
			case 'f':
				e.MoveTo(e.right(e.cursor, true), shift)
			default:
				return false
			}
			return true
		}
		e.insert(string(ev.Rune()), true)
	case tcell.KeyEnter, tcell.KeyCtrlJ:
		e.newline()
	case tcell.KeyTab:
		e.tab()
	case tcell.KeyBacktab:
		e.indent(true)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		e.deleteBackward(word)
	case tcell.KeyDelete:
		e.deleteForward(word)
	case tcell.KeyLeft:
		if !shift && !word && !e.selectionEmpty() {
			start, _ := e.Selection()
			e.MoveTo(start, false)
		} else {
			e.MoveTo(e.left(e.cursor, word), shift)
		}
	case tcell.KeyRight:
		if !shift && !word && !e.selectionEmpty() {
			_, end := e.Selection()
			e.MoveTo(end, false)
		} else {
			e.MoveTo(e.right(e.cursor, word), shift)
		}
	case tcell.KeyUp:
		e.moveVertical(-1, shift)
	case tcell.KeyDown:
		e.moveVertical(1, shift)
	case tcell.KeyPgUp:
		e.moveVertical(-max(1, e.h-1), shift)
	case tcell.KeyPgDn:
		e.moveVertical(max(1, e.h-1), shift)
	case tcell.KeyHome:
		if mods&tcell.ModCtrl != 0 {
			e.MoveTo(Pos{}, shift)
		} else {
			e.MoveTo(e.home(e.cursor), shift)
		}
	case tcell.KeyEnd:
		if mods&tcell.ModCtrl != 0 {
			e.MoveTo(e.buf.End(), shift)
		} else {
			e.MoveTo(at(e.cursor.Row, e.lineLen(e.cursor.Row)), shift)
		}
	default:
		return false
	}
	return true
}

// shortcut はエディタ自身の Ctrl ショートカット。
func (e *Editor) shortcut(ev *tcell.EventKey) func() {
	shortcuts := map[rune]func(){
		'z': e.Undo,
		'y': e.Redo,
		'a': func() { e.Select(Pos{}, e.buf.End()) },
		'c': e.copySelection,
		'x': e.cut,
		'v': func() { e.paste(e.clipboard.Paste()) },
		't': func() { e.ToggleCheckbox(e.cursor.Row) },
		'w': func() { e.deleteBackward(true) },
		'k': e.killLine,
	}
	for letter, action := range shortcuts {
		if IsCtrl(ev, letter) {
			return action
		}
	}
	return nil
}

// isCtrlCombo は Ctrl+文字か (Tab・Enter・Backspace は制御文字と同じ値なので除く)。
func isCtrlCombo(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyTab, tcell.KeyEnter, tcell.KeyBackspace, tcell.KeyCtrlJ, tcell.KeyEsc:
		return false
	}
	if ev.Key() >= tcell.KeyCtrlA && ev.Key() <= tcell.KeyCtrlZ {
		return true
	}
	return ev.Key() == tcell.KeyRune && ev.Modifiers()&tcell.ModCtrl != 0
}

// killLine はカーソルから行末までを消す。行末なら改行を消す。
func (e *Editor) killLine() {
	end := at(e.cursor.Row, e.lineLen(e.cursor.Row))
	if e.cursor == end {
		end = e.right(end, false)
	}
	e.deleteRange(e.cursor, end)
}

// HandleMouse はエディタ内のマウス操作。x, y は画面上の位置。
func (e *Editor) HandleMouse(ev *tcell.EventMouse) {
	x, y := ev.Position()
	buttons := ev.Buttons()
	switch {
	case buttons&tcell.WheelUp != 0:
		e.Scroll(-wheelRows)
	case buttons&tcell.WheelDown != 0:
		e.Scroll(wheelRows)
	case buttons&tcell.Button1 != 0:
		p := e.posAt(x-e.x, max(0, min(y-e.y, e.h-1)))
		if e.mouse.pressed {
			e.MoveTo(p, true) // ドラッグで選択
			return
		}
		e.mouse.pressed = true
		if e.concealed(p.Row) && e.checkboxAt(p.Row, x-e.textX) {
			e.ToggleCheckbox(p.Row)
			return
		}
		now := time.Now()
		if now.Sub(e.mouse.lastClick) < doubleClick && e.mouse.lastPos == p {
			start, end := e.wordAt(p)
			e.Select(start, end)
		} else {
			e.MoveTo(p, ev.Modifiers()&tcell.ModShift != 0)
		}
		e.mouse.lastClick, e.mouse.lastPos = now, p
	default:
		e.mouse.pressed = false
	}
}

// Dragging はドラッグ中か (エディタの外に出てもマウスを受け取り続ける)。
func (e *Editor) Dragging() bool { return e.mouse.pressed }
