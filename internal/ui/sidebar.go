package ui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/Mojashi/memo/internal/notes"
	"github.com/Mojashi/memo/internal/theme"
	"github.com/Mojashi/memo/internal/vault"
)

const sidebarWidth = 30

type sidebarTab int

const (
	filesTab sidebarTab = iota
	outlineTab
)

// treeRow はファイル一覧の1行。
type treeRow struct {
	entry vault.Entry
	depth int
	up    bool // 親フォルダへ移る行
}

// sidebar はファイル一覧と目次。
type sidebar struct {
	vault    *vault.Vault
	tab      sidebarTab
	expanded map[string]bool
	rows     []treeRow
	headings []notes.Heading
	selected int
	scroll   int
	area     rect // 一覧の範囲 (タブを除く)
	tabs     [2]rect

	openFile    func(path string)
	jumpToLine  func(line int)
	currentPath func() string
}

func newSidebar(v *vault.Vault) *sidebar {
	s := &sidebar{vault: v, expanded: map[string]bool{}}
	s.reload()
	return s
}

// reload はフォルダの中身を読み直す。
func (s *sidebar) reload() {
	s.rows = s.rows[:0]
	if parent := filepath.Dir(s.vault.Root); parent != s.vault.Root {
		s.rows = append(s.rows, treeRow{entry: vault.Entry{Path: parent, Name: "..", IsDir: true}, up: true})
	}
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		for _, e := range s.vault.List(dir) {
			s.rows = append(s.rows, treeRow{entry: e, depth: depth})
			if e.IsDir && s.expanded[e.Path] {
				walk(e.Path, depth+1)
			}
		}
	}
	walk(s.vault.Root, 0)
	s.selected = min(s.selected, max(0, s.count()-1))
}

// reveal は path までのフォルダを開き、その行を選ぶ。
func (s *sidebar) reveal(path string) {
	for dir := filepath.Dir(path); strings.HasPrefix(dir, s.vault.Root) && dir != s.vault.Root; dir = filepath.Dir(dir) {
		s.expanded[dir] = true
	}
	s.reload()
	for i, r := range s.rows {
		if r.entry.Path == path {
			s.selected = i
		}
	}
}

func (s *sidebar) count() int {
	if s.tab == filesTab {
		return len(s.rows)
	}
	return len(s.headings)
}

func (s *sidebar) activate(index int) {
	if index < 0 || index >= s.count() {
		return
	}
	s.selected = index
	if s.tab == outlineTab {
		s.jumpToLine(s.headings[index].Line)
		return
	}
	if s.rows[index].up {
		s.goUp()
		return
	}
	e := s.rows[index].entry
	if e.IsDir {
		s.expanded[e.Path] = !s.expanded[e.Path]
		s.reload()
		return
	}
	s.openFile(e.Path)
}

// goUp は一覧の根を親フォルダに移す。今いたフォルダは開いたままにする。
func (s *sidebar) goUp() {
	previous := s.vault.Root
	parent := filepath.Dir(previous)
	if parent == previous {
		return
	}
	s.vault.Root = parent
	s.expanded[previous] = true
	s.reload()
	for i, r := range s.rows {
		if r.entry.Path == previous {
			s.selected = i
		}
	}
}

func (s *sidebar) draw(scr tcell.Screen, r rect, t theme.Theme, focused bool) {
	base := theme.Style{Fg: t.Foreground, Bg: t.Surface}
	fill(scr, r, base)
	for y := r.y; y < r.y+r.h; y++ {
		put(scr, r.x+r.w-1, y, 1, "│", theme.Style{Fg: t.Panel, Bg: t.Surface})
	}
	r.w--

	// タブ
	x := r.x + 1
	for i, label := range []string{" ファイル ", " 目次 "} {
		style := base.Over(t.Faint())
		if sidebarTab(i) == s.tab {
			style = theme.Style{Fg: t.Accent, Bg: t.Surface, Bold: true, Underline: true}
		}
		start := x
		x = put(scr, x, r.y, r.w-(x-r.x), label, style)
		s.tabs[i] = rect{start, r.y, x - start, 1}
		x++
	}

	if s.tab == filesTab {
		put(scr, r.x+1, r.y+1, r.w-1, "▣ "+shortPath(s.vault.Root, r.w-4), base.Over(theme.Style{Fg: t.Muted(), Italic: true}))
	}
	s.area = rect{r.x, r.y + 2, r.w, r.h - 2}
	if s.selected < s.scroll {
		s.scroll = s.selected
	}
	if s.selected >= s.scroll+s.area.h {
		s.scroll = s.selected - s.area.h + 1
	}
	current := s.currentPath()
	for row := 0; row < s.area.h && s.scroll+row < s.count(); row++ {
		index := s.scroll + row
		y := s.area.y + row
		style := base
		if index == s.selected && focused {
			style.Bg = t.Selection()
		}
		var segments []segment
		if s.tab == filesTab {
			tr := s.rows[index]
			indent := strings.Repeat("  ", tr.depth)
			switch {
			case tr.up:
				segments = []segment{{"↑ ", t.Faint()}, {"..", theme.Style{Fg: t.Secondary, Bold: true}}, {"  親フォルダ", t.Faint()}}
			case tr.entry.IsDir && s.expanded[tr.entry.Path]:
				segments = []segment{{indent + "▾ ", t.Faint()}, {tr.entry.Name, theme.Style{Fg: t.Secondary, Bold: true}}}
			case tr.entry.IsDir:
				segments = []segment{{indent + "▸ ", t.Faint()}, {tr.entry.Name, theme.Style{Fg: t.Secondary, Bold: true}}}
			case tr.entry.Path == current:
				segments = []segment{{indent + "  ", theme.Style{}}, {tr.entry.Name, theme.Style{Fg: t.Primary, Bold: true}}}
			case vault.IsMarkdown(tr.entry.Path):
				segments = []segment{{indent + "  ", theme.Style{}}, {tr.entry.Name, theme.Style{}}}
			default:
				segments = []segment{{indent + "  ", theme.Style{}}, {tr.entry.Name, t.Faint()}}
			}
		} else {
			h := s.headings[index]
			mark := "◆ "
			markStyle := theme.Style{Fg: t.Accent}
			if h.Level > 1 {
				mark, markStyle = "▸ ", t.Faint()
			}
			segments = []segment{{strings.Repeat("  ", h.Level-1) + mark, markStyle}, {h.Title, theme.Style{Bold: h.Level == 1}}}
		}
		fill(scr, rect{s.area.x, y, s.area.w, 1}, style)
		putSegments(scr, s.area.x+1, y, s.area.w-1, style, segments...)
	}
	if s.count() == 0 && s.tab == outlineTab {
		put(scr, s.area.x+1, s.area.y, s.area.w-1, "見出しがありません", base.Over(t.Faint()))
	}
}

// handleKey はサイドバーにフォーカスがあるときのキー操作。
func (s *sidebar) handleKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyUp:
		s.selected = max(0, s.selected-1)
	case tcell.KeyDown:
		s.selected = min(max(0, s.count()-1), s.selected+1)
	case tcell.KeyEnter:
		s.activate(s.selected)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if s.tab == filesTab {
			s.goUp()
		}
	case tcell.KeyTab:
		s.tab = 1 - s.tab
		s.selected, s.scroll = 0, 0
	case tcell.KeyLeft, tcell.KeyRight:
		if s.tab == filesTab && s.selected < len(s.rows) {
			e := s.rows[s.selected].entry
			if e.IsDir && s.expanded[e.Path] != (ev.Key() == tcell.KeyRight) {
				s.activate(s.selected)
			}
		}
	default:
		return false
	}
	return true
}

func (s *sidebar) handleMouse(ev *tcell.EventMouse) {
	x, y := ev.Position()
	switch {
	case ev.Buttons()&tcell.WheelUp != 0:
		s.scroll = max(0, s.scroll-3)
		s.selected = min(s.selected, s.scroll+s.area.h-1)
		s.selected = max(s.selected, s.scroll)
	case ev.Buttons()&tcell.WheelDown != 0:
		s.scroll = min(max(0, s.count()-s.area.h), s.scroll+3)
		s.selected = max(s.selected, s.scroll)
	case ev.Buttons()&tcell.Button1 != 0:
		for i, tab := range s.tabs {
			if tab.contains(x, y) {
				s.tab = sidebarTab(i)
				s.selected, s.scroll = 0, 0
				return
			}
		}
		if s.area.contains(x, y) {
			s.activate(s.scroll + y - s.area.y)
		}
	}
}

// shortPath はホームを ~ にし、幅に収まらなければ先頭を「…」で省く。
func shortPath(path string, width int) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		path = "~" + path[len(home):]
	}
	runes := []rune(path)
	for len(runes) > 1 && textWidth(string(runes)) > width {
		runes = runes[1:]
		if textWidth("…"+string(runes)) <= width {
			return "…" + string(runes)
		}
	}
	return string(runes)
}
