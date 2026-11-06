package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// harness は画面を出さずに memo を操作する。
type harness struct {
	t      *testing.T
	screen tcell.SimulationScreen
	app    *App
}

func start(t *testing.T, files map[string]string, open string) *harness {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(100, 30)
	target := dir
	if open != "" {
		target = filepath.Join(dir, open)
	}
	app, err := newApp(screen, target)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t, screen, app}
	app.draw()
	return h
}

// rows は画面の各行の文字列 (全角文字の2マス目は詰める)。
func (h *harness) rows() []string {
	cells, w, height := h.screen.GetContents()
	rows := make([]string, height)
	for y := range height {
		var b strings.Builder
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			if len(c.Runes) == 0 {
				continue
			}
			b.WriteString(string(c.Runes))
			x += textWidth(string(c.Runes)) - 1
		}
		rows[y] = strings.TrimRight(b.String(), " ")
	}
	return rows
}

func (h *harness) screenText() string { return strings.Join(h.rows(), "\n") }

func (h *harness) contains(want string) {
	h.t.Helper()
	if !strings.Contains(h.screenText(), want) {
		h.t.Errorf("画面に %q がありません:\n%s", want, h.screenText())
	}
}

func (h *harness) lacks(unwanted string) {
	h.t.Helper()
	if strings.Contains(h.screenText(), unwanted) {
		h.t.Errorf("画面に %q があってはいけません:\n%s", unwanted, h.screenText())
	}
}

func (h *harness) key(k tcell.Key, mod tcell.ModMask) {
	h.app.handleKey(tcell.NewEventKey(k, 0, mod))
	h.app.draw()
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		if r == '\n' {
			h.app.handleKey(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
		} else {
			h.app.handleKey(tcell.NewEventKey(tcell.KeyRune, r, 0))
		}
	}
	h.app.draw()
}

func (h *harness) click(x, y int) {
	h.app.handleMouse(tcell.NewEventMouse(x, y, tcell.Button1, 0))
	h.app.handleMouse(tcell.NewEventMouse(x, y, tcell.ButtonNone, 0))
	h.app.draw()
}

func TestWelcomeRendersLivePreview(t *testing.T) {
	h := start(t, nil, "")
	h.screen.SetSize(100, 50)
	h.app.draw()
	h.contains("ようこそ memo へ ✎")
	h.contains("☑ チェックリスト")
	h.contains("太字 や 斜体、code、リンク は")
	h.contains("│ 操作 │")
	h.lacks("**太字**")
	t.Log("\n" + h.screenText())
}

func TestCursorLineRevealsMarkdown(t *testing.T) {
	h := start(t, map[string]string{"a.md": "# 見出し\n\n**太字** です\n"}, "a.md")
	h.contains("# 見出し") // カーソル行は記号ごと見える
	h.contains("太字 です")
	h.key(tcell.KeyDown, 0)
	h.key(tcell.KeyDown, 0)
	h.contains("**太字** です")
	h.lacks("# 見出し")
}

func TestTypingJapaneseAndSaving(t *testing.T) {
	h := start(t, map[string]string{"a.md": ""}, "a.md")
	h.typeText("日本語を打つ\n- [ ] やること")
	h.key(tcell.KeyEnter, 0)
	h.typeText("次")
	h.key(tcell.KeyCtrlS, tcell.ModCtrl)
	data, err := os.ReadFile(h.app.note.path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "日本語を打つ\n- [ ] やること\n- [ ] 次"; got != want {
		t.Errorf("保存内容\n got %q\nwant %q", got, want)
	}
}

func TestOptionArrowMovesByWord(t *testing.T) {
	h := start(t, map[string]string{"a.md": "hello brave new world"}, "a.md")
	h.key(tcell.KeyRight, tcell.ModAlt)
	h.key(tcell.KeyRight, tcell.ModAlt)
	if got := h.app.editor.Cursor().Col; got != 11 {
		t.Errorf("Option+→ 2回で列 11 のはず: %d", got)
	}
	h.key(tcell.KeyLeft, tcell.ModAlt)
	if got := h.app.editor.Cursor().Col; got != 6 {
		t.Errorf("Option+← で列 6 のはず: %d", got)
	}
	h.app.handleKey(tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModAlt))
	if got := h.app.editor.Cursor().Col; got != 0 {
		t.Errorf("Option+b で列 0 のはず: %d", got)
	}
}

func TestWideTableStaysOnOneRowPerLine(t *testing.T) {
	long := strings.Repeat("とても長い説明", 12)
	h := start(t, map[string]string{"a.md": "カーソル\n\n| 名前 | 説明 |\n| --- | --- |\n| a | " + long + " |\n"}, "a.md")
	h.contains("…")
	for _, r := range h.rows() {
		if strings.Contains(r, "とても長い") && !strings.Contains(r, "│ a") {
			t.Errorf("表のセルが折り返されています:\n%s", h.screenText())
		}
	}
}

func TestClickCheckboxToggles(t *testing.T) {
	h := start(t, map[string]string{"a.md": "先頭\n- [ ] やる\n"}, "a.md")
	var x, y int
	for row, text := range h.rows() {
		if before, _, found := strings.Cut(text, "☐"); found {
			x, y = textWidth(before), row
		}
	}
	h.click(x, y)
	if got := h.app.editor.Text(); got != "先頭\n- [x] やる\n" {
		t.Errorf("チェックが切り替わっていません: %q", got)
	}
}

func TestCodeFileHasLineNumbers(t *testing.T) {
	h := start(t, map[string]string{"main.py": "def f():\n    return 1\n"}, "main.py")
	h.contains("1  def f():")
	h.contains("2      return 1")
	h.lacks(" B ") // 書式ツールバーは出さない
}

func TestUndoRestoresText(t *testing.T) {
	h := start(t, map[string]string{"a.md": "abc"}, "a.md")
	h.typeText("xyz")
	h.key(tcell.KeyCtrlZ, tcell.ModCtrl)
	if got := h.app.editor.Text(); got != "abc" {
		t.Errorf("元に戻っていません: %q", got)
	}
	h.key(tcell.KeyCtrlY, tcell.ModCtrl)
	if got := h.app.editor.Text(); got != "xyzabc" {
		t.Errorf("やり直せていません: %q", got)
	}
}
