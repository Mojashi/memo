// Package ui は memo の画面とその振る舞い。
//
// 画面構成:
//
//	タイトルバー / 書式ツールバー
//	サイドバー (ファイル・目次) | エディタ
//	ステータスバー / キー一覧
//
// 振る舞い:
//   - Markdown はエディタ自体が完成形で描く (ライブプレビュー)。他のテキストファイルは色付きのコードとして開く
//   - 別のノートを開く前・終了する前に、編集中のノートを保存する
//   - 入力が落ち着いたら目次・統計・コードの色を更新し、ファイルなら自動保存する
package ui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/Mojashi/memo/internal/editor"
	"github.com/Mojashi/memo/internal/format"
	"github.com/Mojashi/memo/internal/notes"
	"github.com/Mojashi/memo/internal/syntax"
	"github.com/Mojashi/memo/internal/theme"
	"github.com/Mojashi/memo/internal/vault"
)

const (
	settleDelay   = 350 * time.Millisecond
	autosaveDelay = 1500 * time.Millisecond
	toastDuration = 2500 * time.Millisecond
	zenWidth      = 96
)

// note はエディタに開いているノート。Path が空なら未命名。
type note struct {
	path  string
	saved string // 最後に保存した (または開いた) 内容
	crlf  bool   // 元の改行が CRLF なら書き戻すときも CRLF にする
}

type focus int

const (
	focusEditor focus = iota
	focusSidebar
)

type toast struct {
	text  string
	error bool
	until time.Time
}

type App struct {
	screen  tcell.Screen
	vault   *vault.Vault
	theme   theme.Theme
	editor  *editor.Editor
	sidebar *sidebar
	note    note
	focus   focus
	dialogs []dialog
	toasts  []toast
	quit    bool

	showSidebar bool
	zen         bool
	raw         bool // Markdown の記号を全部見せる

	stats      notes.Stats
	settleGen  int
	saveGen    int
	searchGen  atomic.Int64 // 検索のたびに増える。古い検索は打ち切る
	toolbar    []toolButton
	editorArea rect
	sideArea   rect
	dirtyCache struct {
		version int
		dirty   bool
	}
}

// callEvent はメインループで実行してほしい処理 (タイマーや別スレッドから)。
type callEvent struct {
	tcell.EventTime
	fn func()
}

// Run は path (フォルダかファイル) を開いて memo を起動する。
func Run(path string) error {
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()
	a, err := newApp(screen, path)
	if err != nil {
		return err
	}
	a.loop()
	return nil
}

func newApp(screen tcell.Screen, path string) (*App, error) {
	v, initial, err := resolveTarget(path)
	if err != nil {
		return nil, err
	}
	conf, err := loadSettings()
	if err != nil {
		return nil, fmt.Errorf("設定を読めません: %w", err)
	}
	screen.EnableMouse(tcell.MouseButtonEvents | tcell.MouseDragEvents)
	screen.EnablePaste()
	screen.SetCursorStyle(tcell.CursorStyleSteadyBar)

	a := &App{screen: screen, vault: v, theme: theme.Named(conf.Theme), showSidebar: true}
	a.editor = editor.New(&clipboard{screen: screen}, a.theme)
	a.editor.OnChange = a.textChanged
	a.sidebar = newSidebar(v)
	a.sidebar.openFile = a.openNote
	a.sidebar.jumpToLine = a.jumpTo
	a.sidebar.currentPath = func() string { return a.note.path }

	if initial != "" {
		a.openNote(initial)
	} else {
		a.show(note{}, welcome)
	}
	return a, nil
}

// resolveTarget は引数がフォルダならそこを vault に、ファイルならその親を vault にしてそのファイルを開く。
func resolveTarget(arg string) (*vault.Vault, string, error) {
	if arg == "" {
		arg = "."
	}
	if strings.HasPrefix(arg, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, "", err
		}
		arg = filepath.Join(home, arg[2:])
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return nil, "", err
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		v, err := vault.Open(abs)
		return v, "", err
	}
	v, err := vault.Open(filepath.Dir(abs))
	return v, abs, err
}

func (a *App) loop() {
	for !a.quit {
		a.draw()
		switch ev := a.screen.PollEvent().(type) {
		case nil:
			return
		case *tcell.EventResize:
			a.screen.Sync()
		case *tcell.EventKey:
			a.handleKey(ev)
		case *tcell.EventMouse:
			a.handleMouse(ev)
		case *tcell.EventPaste:
			a.handlePaste(ev)
		case *callEvent:
			ev.fn()
		}
	}
}

// later は d 後にメインループで fn を実行する。
func (a *App) later(d time.Duration, fn func()) {
	time.AfterFunc(d, func() { a.post(fn) })
}

// post は別スレッドからメインループに fn を渡す。
func (a *App) post(fn func()) {
	ev := &callEvent{fn: fn}
	ev.SetEventNow()
	_ = a.screen.PostEvent(ev)
}

func (a *App) notify(format string, args ...any) {
	a.toasts = append(a.toasts, toast{text: fmt.Sprintf(format, args...), until: time.Now().Add(toastDuration)})
	a.later(toastDuration, func() {})
}

func (a *App) fail(err error) {
	a.toasts = append(a.toasts, toast{text: err.Error(), error: true, until: time.Now().Add(2 * toastDuration)})
	a.later(2*toastDuration, func() {})
}

// ── ノートを開く・保存する ──────────────────────────────

func (a *App) openNote(path string) {
	text, err := vault.ReadText(path)
	if err != nil {
		a.fail(err)
		return
	}
	a.saveIfNeeded()
	crlf := strings.Contains(text, "\r\n")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	a.show(note{path: path, saved: text, crlf: crlf}, text)
}

func (a *App) show(n note, text string) {
	a.note = n
	markdown := vault.IsMarkdown(n.path)
	var lexer = syntax.ForFile(n.path)
	if markdown {
		lexer = nil
	}
	a.editor.Load(text, markdown, lexer)
	a.editor.SetLive(markdown && !a.raw)
	a.focus = focusEditor
	if n.path != "" {
		a.sidebar.reveal(n.path)
	}
	a.screen.SetTitle("memo — " + a.noteName())
	a.settle()
}

func (a *App) noteName() string {
	if a.note.path == "" {
		return "未命名"
	}
	return a.vault.Relative(a.note.path)
}

func (a *App) dirty() bool {
	version := a.editor.Version()
	if a.dirtyCache.version != version {
		a.dirtyCache.version = version
		a.dirtyCache.dirty = a.editor.Text() != a.note.saved
	}
	return a.dirtyCache.dirty
}

func (a *App) saveIfNeeded() {
	if a.note.path != "" && a.dirty() {
		a.writeNote(a.note.path)
	}
}

func (a *App) writeNote(path string) bool {
	text := a.editor.Text()
	data := text
	if a.note.crlf {
		data = strings.ReplaceAll(text, "\n", "\r\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		a.fail(err)
		return false
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		a.fail(err)
		return false
	}
	isNewFile := a.note.path != path
	a.note = note{path: path, saved: text, crlf: a.note.crlf}
	a.dirtyCache.version = -1
	if isNewFile {
		a.sidebar.reveal(path)
		a.screen.SetTitle("memo — " + a.noteName())
	}
	return true
}

func (a *App) save() {
	if a.note.path != "" {
		if a.writeNote(a.note.path) {
			a.notify("%s を保存しました", a.noteName())
		}
		return
	}
	a.prompt("名前を付けて保存", "例: ideas/"+time.Now().Format("2006-01-02")+".md", "", func(name string) {
		if a.writeNote(a.vault.PathFor(name)) {
			a.notify("保存しました")
		}
	})
}

func (a *App) newNote() {
	a.prompt("新しいノート", "ノート名 (フォルダ/名前 も可)", "", func(name string) {
		path, err := a.vault.Create(name)
		if err != nil {
			a.fail(err)
			return
		}
		a.openNote(path)
		a.editor.MoveTo(a.editor.End(), false)
	})
}

func (a *App) rename() {
	if a.note.path == "" {
		a.save()
		return
	}
	a.prompt("名前を変更", "", a.noteName(), func(name string) {
		a.saveIfNeeded()
		renamed, err := a.vault.Rename(a.note.path, name)
		if err != nil {
			a.fail(err)
			return
		}
		a.note.path = renamed
		a.sidebar.reveal(renamed)
		a.screen.SetTitle("memo — " + a.noteName())
	})
}

func (a *App) requestQuit() {
	if a.note.path != "" {
		a.saveIfNeeded()
		a.quit = true
		return
	}
	if !a.dirty() || a.editor.Text() == welcome {
		a.quit = true
		return
	}
	a.confirm("未命名のメモは保存されていません。破棄して終了しますか？", "破棄して終了", func() { a.quit = true })
}

// ── 入力が落ち着いたら ──────────────────────────────────

func (a *App) textChanged() {
	a.settleGen++
	gen := a.settleGen
	a.later(settleDelay, func() {
		if gen == a.settleGen {
			a.settle()
		}
	})
	if a.note.path != "" {
		a.saveGen++
		gen := a.saveGen
		a.later(autosaveDelay, func() {
			if gen == a.saveGen {
				a.saveIfNeeded()
			}
		})
	}
}

func (a *App) settle() {
	lines := a.editor.Lines()
	a.stats = notes.Measure(a.editor.Text())
	if a.editor.IsMarkdown() {
		a.sidebar.headings = notes.Outline(lines)
	} else {
		a.sidebar.headings = nil
	}
	a.editor.Recolor()
}

// ── 移動・検索 ──────────────────────────────────────────

func (a *App) jumpTo(line int) {
	a.editor.JumpTo(line)
	a.focusEditor()
}

func (a *App) focusEditor() { a.focus = focusEditor }

func (a *App) search() {
	var picker *pickerDialog
	picker = newPicker("全ノートを検索", "キーワード (ファイル名・本文)", 110, func(query string) {
		gen := a.searchGen.Add(1)
		if strings.TrimSpace(query) == "" {
			picker.setItems(nil)
			return
		}
		go func() {
			hits := a.vault.Search(query, 200, func() bool { return gen != a.searchGen.Load() })
			a.post(func() {
				if gen == a.searchGen.Load() {
					picker.setItems(a.hitItems(hits, query))
				}
			})
		}()
	})
	a.openDialog(picker)
}

func (a *App) hitItems(hits []vault.Hit, query string) []pickItem {
	items := make([]pickItem, len(hits))
	for i, hit := range hits {
		label := []segment{{a.vault.Relative(hit.Path), theme.Style{Bold: true}}}
		if hit.Line >= 0 {
			label = append(label, segment{fmt.Sprintf("  %d: ", hit.Line+1), a.theme.Faint()})
			label = append(label, highlight(truncate(hit.Text, 120), query, theme.Style{Fg: a.theme.Background, Bg: a.theme.Warning})...)
		}
		items[i] = pickItem{label: label, choose: func() {
			if hit.Path != a.note.path {
				a.openNote(hit.Path)
			}
			if hit.Line >= 0 {
				a.jumpTo(hit.Line)
			}
		}}
	}
	return items
}

// quickOpen はファイル名の一部を打ってファイルを開く。
func (a *App) quickOpen() {
	var files []string
	a.vault.Files(func(path string) bool {
		files = append(files, path)
		return len(files) < 20000
	})
	var picker *pickerDialog
	picker = newPicker("ファイルを開く", "ファイル名の一部", 80, func(query string) {
		var items []pickItem
		for _, path := range files {
			rel := a.vault.Relative(path)
			if !fuzzyMatch(strings.ToLower(rel), strings.ToLower(query)) {
				continue
			}
			style := theme.Style{}
			if !vault.IsMarkdown(path) {
				style = a.theme.Faint()
			}
			items = append(items, pickItem{label: []segment{{rel, style}}, choose: func() { a.openNote(path) }})
			if len(items) >= 500 {
				break
			}
		}
		picker.setItems(items)
	})
	picker.refresh("")
	a.openDialog(picker)
}

// fuzzyMatch は query の文字が順に text に含まれるか。
func fuzzyMatch(text, query string) bool {
	for _, r := range query {
		i := strings.IndexRune(text, r)
		if i < 0 {
			return false
		}
		text = text[i+len(string(r)):]
	}
	return true
}

// ── 書式・表示切替 ──────────────────────────────────────

func (a *App) applyFormat(id string) {
	if !a.editor.IsMarkdown() {
		return
	}
	a.editor.ApplyFormat(format.ByID(id).Format)
	a.focusEditor()
}

func (a *App) toggleSidebar() {
	a.showSidebar = !a.showSidebar
	if !a.showSidebar {
		a.focusEditor()
	}
}

func (a *App) toggleZen() {
	a.zen = !a.zen
	a.focusEditor()
}

func (a *App) toggleRaw() {
	a.raw = !a.raw
	a.editor.SetLive(a.editor.IsMarkdown() && !a.raw)
	if a.raw {
		a.notify("記号をすべて表示")
	} else {
		a.notify("ライブプレビュー")
	}
}

func (a *App) focusSidebar() {
	if a.focus == focusSidebar {
		a.focusEditor()
		return
	}
	a.showSidebar = true
	a.zen = false
	a.focus = focusSidebar
}

func (a *App) setTheme(t theme.Theme) {
	a.theme = t
	a.editor.SetTheme(t)
	if err := saveSettings(settings{Theme: t.Name}); err != nil {
		a.fail(err)
	}
}

// command はコマンドパレットの1項目。
type command struct {
	name, detail string
	run          func()
}

func (a *App) commands() []command {
	cmds := []command{
		{"新しいノート", "Ctrl+N", a.newNote},
		{"ファイルを開く", "Ctrl+O", a.quickOpen},
		{"保存", "Ctrl+S", a.save},
		{"名前を変更", "", a.rename},
		{"全ノートを検索", "Ctrl+F", a.search},
		{"サイドバー表示切替", "F2", a.toggleSidebar},
		{"サイドバーへ移動", "Ctrl+E", a.focusSidebar},
		{"集中モード", "F4", a.toggleZen},
		{"記号表示切替", "F6  Markdown の記号を全部見せる / 完成形で見せる", a.toggleRaw},
	}
	for _, t := range theme.All {
		cmds = append(cmds, command{"テーマ: " + t.Name, "", func() { a.setTheme(t) }})
	}
	for _, f := range format.Actions {
		cmds = append(cmds, command{"書式: " + f.Description, f.Label, func() { a.applyFormat(f.ID) }})
	}
	return append(cmds, command{"終了", "Ctrl+Q", a.requestQuit})
}

func (a *App) commandPalette() {
	cmds := a.commands()
	var picker *pickerDialog
	picker = newPicker("コマンド", "コマンド名", 72, func(query string) {
		var items []pickItem
		for _, c := range cmds {
			if fuzzyMatch(strings.ToLower(c.name+" "+c.detail), strings.ToLower(query)) {
				items = append(items, pickItem{label: []segment{{c.name, theme.Style{}}}, detail: c.detail, choose: c.run})
			}
		}
		picker.setItems(items)
	})
	picker.refresh("")
	a.openDialog(picker)
}

// ── ダイアログ ──────────────────────────────────────────

func (a *App) openDialog(d dialog) { a.dialogs = append(a.dialogs, d) }

func (a *App) closeDialog(d dialog) {
	for i, x := range a.dialogs {
		if x == d {
			a.dialogs = append(a.dialogs[:i], a.dialogs[i+1:]...)
			return
		}
	}
}

func (a *App) prompt(title, placeholder, value string, done func(string)) {
	var d *promptDialog
	d = newPrompt(title, placeholder, value, func(v string, ok bool) {
		a.closeDialog(d)
		if ok {
			done(strings.TrimSpace(v))
		}
	})
	a.openDialog(d)
}

func (a *App) confirm(message, label string, yes func()) {
	var d *confirmDialog
	d = &confirmDialog{message: message, confirm: label, done: func(ok bool) {
		a.closeDialog(d)
		if ok {
			yes()
		}
	}}
	a.openDialog(d)
}

// ── キー・マウス ────────────────────────────────────────

// shortcut はアプリ全体のショートカット。
func (a *App) shortcut(ev *tcell.EventKey) func() {
	switch ev.Key() {
	case tcell.KeyF2:
		return a.toggleSidebar
	case tcell.KeyF4:
		return a.toggleZen
	case tcell.KeyF6:
		return a.toggleRaw
	}
	shortcuts := []struct {
		letter rune
		run    func()
	}{
		{'s', a.save},
		{'n', a.newNote},
		{'o', a.quickOpen},
		{'f', a.search},
		{'p', a.commandPalette},
		{'e', a.focusSidebar},
		{'q', a.requestQuit},
		{'b', func() { a.applyFormat("bold") }},
		{'l', func() { a.applyFormat("link") }},
	}
	for _, s := range shortcuts {
		if editor.IsCtrl(ev, s.letter) {
			return s.run
		}
	}
	return nil
}

func (a *App) handleKey(ev *tcell.EventKey) {
	if n := len(a.dialogs); n > 0 {
		d := a.dialogs[n-1]
		if d.handleKey(ev) {
			a.closeDialog(d)
		}
		return
	}
	if run := a.shortcut(ev); run != nil {
		run()
		return
	}
	if a.focus == focusSidebar {
		if ev.Key() == tcell.KeyEsc {
			a.focusEditor()
			return
		}
		a.sidebar.handleKey(ev)
		return
	}
	a.editor.HandleKey(ev)
}

func (a *App) handlePaste(ev *tcell.EventPaste) {
	if len(a.dialogs) > 0 {
		// ダイアログへの貼り付けは、間に届く文字キーがそのまま入力される
		return
	}
	if ev.Start() {
		a.editor.PasteStart()
	} else {
		a.editor.PasteEnd()
	}
}

func (a *App) handleMouse(ev *tcell.EventMouse) {
	x, y := ev.Position()
	if n := len(a.dialogs); n > 0 {
		d := a.dialogs[n-1]
		if d.handleMouse(ev) {
			a.closeDialog(d)
		}
		return
	}
	if a.editor.Dragging() {
		a.editor.HandleMouse(ev)
		return
	}
	switch {
	case a.editorArea.contains(x, y):
		if ev.Buttons()&tcell.Button1 != 0 {
			a.focusEditor()
		}
		a.editor.HandleMouse(ev)
	case a.showSidebar && !a.zen && a.sideArea.contains(x, y):
		if ev.Buttons()&tcell.Button1 != 0 {
			a.focus = focusSidebar
		}
		a.sidebar.handleMouse(ev)
	case ev.Buttons()&tcell.Button1 != 0:
		for _, b := range a.toolbar {
			if b.area.contains(x, y) {
				b.run()
				return
			}
		}
	}
}

// ── 描画 ────────────────────────────────────────────────

type toolButton struct {
	label string
	style theme.Style
	run   func()
	area  rect
}

func (a *App) draw() {
	s := a.screen
	t := a.theme
	w, h := s.Size()
	a.editor.SetFocused(a.focus == focusEditor && len(a.dialogs) == 0)

	fill(s, rect{0, 0, w, h}, t.Text())
	y := 0
	a.drawTitle(rect{0, y, w, 1})
	y++
	if !a.zen {
		a.drawToolbar(rect{0, y, w, 1})
		y++
	}
	footer := 2
	if a.zen {
		footer = 1
	}
	main := rect{0, y, w, max(1, h-y-footer)}

	if a.showSidebar && !a.zen && main.w > sidebarWidth+20 {
		a.sideArea = rect{main.x, main.y, sidebarWidth, main.h}
		a.sidebar.draw(s, a.sideArea, t, a.focus == focusSidebar)
		main.x += sidebarWidth
		main.w -= sidebarWidth
	} else {
		a.sideArea = rect{}
	}
	if a.zen {
		width := min(main.w, zenWidth)
		main = rect{main.x + (main.w-width)/2, main.y + 1, width, main.h - 1}
	}
	a.editorArea = main
	cx, cy, cursor := a.editor.Draw(s, main.x, main.y, main.w, main.h)

	a.drawStatus(rect{0, h - footer, w, 1})
	if !a.zen {
		a.drawKeys(rect{0, h - 1, w, 1})
	}
	a.drawToasts(rect{0, 0, w, h - footer})

	// ダイアログが開いていればその入力欄に、そうでなければエディタにカーソルを出す。
	// 本来のカーソルを入力位置に出すと、日本語入力の変換中の文字もそこに出る。
	cursor = cursor && a.focus == focusEditor
	for _, d := range a.dialogs {
		cx, cy, cursor = d.draw(s, rect{0, 0, w, h}, t)
	}
	if cursor {
		s.ShowCursor(cx, cy)
	} else {
		s.HideCursor()
	}
	s.Show()
}

func (a *App) drawTitle(r rect) {
	t := a.theme
	base := theme.Style{Fg: t.Background, Bg: t.Primary}
	fill(a.screen, r, base)
	segments := []segment{{"  ✎ memo   ", theme.Style{Bold: true}}, {a.noteName(), theme.Style{Bold: true}}}
	if a.dirty() {
		segments = append(segments, segment{"  ●", theme.Style{Bold: true}})
	}
	putSegments(a.screen, r.x, r.y, r.w, base, segments...)
}

func (a *App) drawToolbar(r rect) {
	t := a.theme
	base := theme.Style{Fg: t.Muted(), Bg: t.Panel}
	fill(a.screen, r, base)
	a.toolbar = a.toolbar[:0]
	x := r.x + 1
	add := func(label string, style theme.Style, run func()) {
		width := put(a.screen, x, r.y, r.x+r.w-x, " "+label+" ", base.Over(style)) - x
		a.toolbar = append(a.toolbar, toolButton{label, style, run, rect{x, r.y, width, 1}})
		x += width
	}
	if a.editor.IsMarkdown() {
		looks := map[string]theme.Style{"bold": {Bold: true}, "italic": {Italic: true}, "strike": {Strike: true}}
		for i, group := range format.ToolbarGroups {
			if i > 0 {
				x = put(a.screen, x, r.y, r.x+r.w-x, " │ ", base.Over(theme.Style{Fg: t.Background}))
			}
			for _, id := range group {
				add(format.ByID(id).Label, theme.Style{Bold: true}.Over(looks[id]), func() { a.applyFormat(id) })
			}
		}
	}
	right := []struct {
		label string
		run   func()
	}{{"◧ サイド", a.toggleSidebar}, {"◎ 集中", a.toggleZen}, {"¶ 記号", a.toggleRaw}}
	width := 0
	for _, b := range right {
		width += textWidth(b.label) + 2
	}
	if x+width < r.x+r.w {
		x = r.x + r.w - width - 1
		for _, b := range right {
			add(b.label, theme.Style{}, b.run)
		}
	}
}

func (a *App) drawStatus(r rect) {
	t := a.theme
	base := theme.Style{Fg: t.Muted(), Bg: t.Panel}
	fill(a.screen, r, base)
	state := segment{" ✓ 保存済み ", theme.Style{Fg: t.Success, Bold: true}}
	if a.dirty() {
		state = segment{" ● 未保存 ", theme.Style{Fg: t.Warning, Bold: true}}
	}
	if a.note.path == "" && a.editor.Text() == welcome {
		state = segment{" ようこそ ", theme.Style{Fg: t.Accent, Bold: true}}
	}
	cursor := a.editor.Cursor()
	sep := segment{"  │  ", theme.Style{Fg: t.Background}}
	minutes := max(1, int(math.Round(a.stats.ReadingMinutes)))
	putSegments(a.screen, r.x, r.y, r.w, base,
		state, sep,
		segment{fmt.Sprintf("行 %d, 列 %d", cursor.Row+1, cursor.Col+1), theme.Style{}}, sep,
		segment{fmt.Sprintf("%s 文字", comma(a.stats.Chars)), theme.Style{}}, sep,
		segment{fmt.Sprintf("%s 語", comma(a.stats.Words)), theme.Style{}}, sep,
		segment{fmt.Sprintf("%s 行", comma(a.stats.Lines)), theme.Style{}}, sep,
		segment{fmt.Sprintf("読了 約%d分", minutes), theme.Style{}},
	)
}

func (a *App) drawKeys(r rect) {
	t := a.theme
	base := theme.Style{Fg: t.Foreground, Bg: t.Surface}
	fill(a.screen, r, base)
	keys := [][2]string{
		{"^S", "保存"}, {"^N", "新規"}, {"^O", "開く"}, {"^F", "検索"}, {"^P", "コマンド"},
		{"^E", "ファイル一覧"}, {"F2", "サイド"}, {"F4", "集中"}, {"F6", "記号"}, {"^Q", "終了"},
	}
	var segments []segment
	for _, k := range keys {
		segments = append(segments, segment{" " + k[0] + " ", theme.Style{Fg: t.Accent, Bold: true}}, segment{k[1] + " ", theme.Style{}})
	}
	putSegments(a.screen, r.x, r.y, r.w, base, segments...)
}

func (a *App) drawToasts(r rect) {
	now := time.Now()
	alive := a.toasts[:0]
	for _, t := range a.toasts {
		if t.until.After(now) {
			alive = append(alive, t)
		}
	}
	a.toasts = alive
	y := r.y + r.h - 1
	for i := len(a.toasts) - 1; i >= 0 && y > r.y; i-- {
		t := a.toasts[i]
		text := " " + truncate(t.text, r.w-6) + " "
		style := theme.Style{Fg: a.theme.Foreground, Bg: a.theme.Panel, Bold: true}
		if t.error {
			style = theme.Style{Fg: a.theme.Background, Bg: a.theme.Error, Bold: true}
		}
		width := textWidth(text)
		put(a.screen, r.x+r.w-width-2, y, width, text, style)
		y--
	}
}
