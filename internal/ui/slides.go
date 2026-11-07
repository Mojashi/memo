package ui

import (
	"errors"
	"fmt"

	"github.com/gdamore/tcell/v2"

	"github.com/Mojashi/memo/internal/graphics"
	"github.com/Mojashi/memo/internal/slides"
	"github.com/Mojashi/memo/internal/theme"
)

// slideImageID は端末に送るスライド画像の id。文字色 (256 色の番号) で表すので 16〜255。
const slideImageID = 238

// slideRenderer はスライドを1枚ずつの PNG にする (複数の memo で共有する描画サーバー)。
type slideRenderer interface {
	Render(source, dir string, status func(string)) ([]string, error)
	Close()
}

// slidePane は Marp の文書で、カーソルのあるスライドを描画サーバーで描いた画像として右側に出す。
//
//   - 入力が落ち着いたら文書全体を描き直す。描いている間に来た依頼は、終わってから最新の1つだけ描く
//   - 画像は kitty の画像表示で出す。出せない端末や描けないときは、理由を代わりに出す
type slidePane struct {
	visible bool
	post    func(func())

	// 初めて Marp の文書を開いたときに調べる
	prepared bool
	term     graphics.Terminal
	termErr  error
	marp     slideRenderer
	marpErr  error

	requested string // 最後に描くよう頼まれたソース
	dir       string
	rendering bool
	queued    bool
	gen       int
	pages     []string
	err       error
	progress  string // 描画サーバーからの途中経過 (Chrome を取ってきているなど)

	shown   shownImage // 端末に送ってある画像
	pending string     // 次に画面を更新したあと、端末へ送る命令
}

type shownImage struct {
	path       string
	cols, rows int
}

func newSlidePane(post func(func())) *slidePane {
	return &slidePane{visible: true, post: post}
}

// prepare は画像を出せる端末か・marp があるかを調べる (一度だけ)。
func (p *slidePane) prepare() {
	if p.prepared {
		return
	}
	p.prepared = true
	p.term, p.termErr = detectTerminal()
	p.marp, p.marpErr = findMarp()
}

// テストで差し替える
var (
	findMarp = func() (slideRenderer, error) {
		client, err := slides.Connect()
		if err != nil {
			return nil, err
		}
		return client, nil
	}
	detectTerminal = graphics.Detect
)

// request は source を描き直すよう頼む。dir は画像などの相対パスの基準。
func (p *slidePane) request(source, dir string) {
	p.prepare()
	if p.problem() != nil || source == p.requested {
		return
	}
	p.requested, p.dir = source, dir
	if p.rendering {
		p.queued = true
		return
	}
	p.start()
}

func (p *slidePane) start() {
	p.rendering = true
	p.gen++
	gen, source, dir := p.gen, p.requested, p.dir
	go func() {
		pages, err := p.marp.Render(source, dir, func(message string) {
			p.post(func() { p.progress = message })
		})
		p.post(func() { p.finished(gen, pages, err) })
	}()
}

func (p *slidePane) finished(gen int, pages []string, err error) {
	p.rendering = false
	p.progress = ""
	p.err = err
	if err == nil && gen == p.gen {
		p.pages = pages
	}
	if p.queued {
		p.queued = false
		p.start()
	}
}

// width は幅 w の画面で、スライドに使う幅 (出さないなら 0)。
func (p *slidePane) width(w int) int {
	if !p.visible || w < 64 {
		return 0
	}
	return min(90, w*9/20)
}

// draw は r にスライド (index 番目 / total 枚) を描く。
func (p *slidePane) draw(s tcell.Screen, r rect, t theme.Theme, index, total int) {
	base := theme.Style{Fg: t.Foreground, Bg: t.Surface}
	fill(s, r, base)
	for y := r.y; y < r.y+r.h; y++ {
		put(s, r.x, y, 1, "│", base.Over(theme.Style{Fg: t.Muted()}))
	}
	inner := rect{r.x + 2, r.y, r.w - 3, r.h}
	status := segment{"", theme.Style{}}
	switch {
	case p.rendering:
		status = segment{"  描いています…", theme.Style{Fg: t.Muted()}}
	case p.err != nil:
		status = segment{"  描けませんでした", theme.Style{Fg: t.Error, Bold: true}}
	}
	putSegments(s, inner.x, inner.y, inner.w, base,
		segment{fmt.Sprintf("▶ スライド %d / %d", index, total), theme.Style{Fg: t.Accent, Bold: true}}, status)

	cols := inner.w
	rows := min(inner.h-2, graphics.MaxRows(), (cols*9+16)/32) // 16:9 を、縦長のマス (幅:高さ ≒ 1:2) で
	message := p.problem()
	if message == nil && p.progress != "" {
		message = errors.New(p.progress)
	}
	if p.problem() == nil && index-1 < len(p.pages) && rows > 0 {
		p.place(s, rect{inner.x, inner.y + 2, cols, rows}, p.pages[index-1], base)
		if p.err != nil {
			message = p.err
		}
		inner.y += rows + 3
		inner.h -= rows + 3
	} else {
		inner.y += 2
		inner.h -= 2
	}
	if message != nil {
		for i, line := range wrapText(message.Error(), inner.w) {
			if i >= inner.h {
				break
			}
			put(s, inner.x, inner.y+i, inner.w, line, base.Over(theme.Style{Fg: t.Muted()}))
		}
	}
}

// problem は画像を出せない理由 (出せるなら nil)。
func (p *slidePane) problem() error {
	if p.marpErr != nil {
		return p.marpErr
	}
	return p.termErr
}

// place は area に画像を置く。端末へ送っていない絵や大きさなら、送る命令を用意する。
func (p *slidePane) place(s tcell.Screen, area rect, path string, base theme.Style) {
	image := shownImage{path, area.w, area.h}
	if image != p.shown {
		p.shown = image
		p.pending = p.term.Show(slideImageID, path, area.w, area.h)
	}
	style := base.Cell().Foreground(tcell.PaletteColor(slideImageID))
	for row := range area.h {
		for col := range area.w {
			s.SetContent(area.x+col, area.y+row, graphics.Placeholder, graphics.Cell(row, col), style)
		}
	}
}

// flush は用意した命令を端末へ送る (画面を更新したあとに呼ぶ)。
func (p *slidePane) flush(s tcell.Screen) {
	if p.pending == "" {
		return
	}
	if tty, ok := s.Tty(); ok {
		_, _ = tty.Write([]byte(p.pending))
	}
	p.pending = ""
}

// close は端末から画像を消し、描画サーバーとのつながりを切る。
func (p *slidePane) close(s tcell.Screen) {
	if p.shown.path != "" {
		p.pending = p.term.Delete(slideImageID)
		p.flush(s)
	}
	if p.marp != nil {
		p.marp.Close()
	}
}
