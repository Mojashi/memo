package ui

import (
	"errors"
	"fmt"

	"github.com/gdamore/tcell/v2"

	"github.com/Mojashi/memo/internal/graphics"
	"github.com/Mojashi/memo/internal/theme"
)

// slidePane は Marp の文書で、カーソルのあるスライドを描画サーバーで描いた画像として右側に出す。
//
//   - 入力が落ち着いたら文書全体を描き直す。描いている間に来た依頼は、終わってから最新の1つだけ描く
//   - 画像は kitty の画像表示で出す。出せない端末や描けないときは、理由を代わりに出す
type slidePane struct {
	visible bool
	post    func(func())
	render  *renderService
	images  *imageLayer

	requested string // 最後に描くよう頼まれたソース
	dir       string
	rendering bool
	queued    bool
	gen       int
	pages     []string
	err       error
	progress  string // 描画サーバーからの途中経過 (Chrome を取ってきているなど)
}

func newSlidePane(post func(func()), render *renderService, images *imageLayer) *slidePane {
	return &slidePane{visible: true, post: post, render: render, images: images}
}

// request は source を描き直すよう頼む。dir は画像などの相対パスの基準。
func (p *slidePane) request(source, dir string) {
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
		r, _ := p.render.get()
		pages, err := r.Render(source, dir, func(message string) {
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
		p.images.place(s, rect{inner.x, inner.y + 2, cols, rows}, slideImageID, p.pages[index-1], base)
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
	if _, err := p.render.get(); err != nil {
		return err
	}
	return p.images.problem()
}
