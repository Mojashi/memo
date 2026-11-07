package ui

import (
	"github.com/gdamore/tcell/v2"

	"github.com/Mojashi/memo/internal/graphics"
	"github.com/Mojashi/memo/internal/slides"
	"github.com/Mojashi/memo/internal/theme"
)

// 端末に送る画像の id。文字色 (256 色の番号) で表すので 16〜255。
const (
	slideImageID = 238 // スライド
	fileImageID  = 237 // 画像ファイル
	pageImageID  = 200 // HTML のページ (画面に並べる k 枚目は pageImageID + k)
	maxPageSlots = 30
)

// テストで差し替える
var (
	findRenderer = func() (renderer, error) {
		client, err := slides.Connect()
		if err != nil {
			return nil, err
		}
		return client, nil
	}
	detectTerminal = graphics.Detect
)

// renderer はスライドや HTML を PNG にする (複数の memo で共有する描画サーバー)。
type renderer interface {
	Render(source, dir string, status func(string)) ([]string, error)
	RenderPage(file string, status func(string)) ([]string, error)
	Close()
}

// renderService は描画サーバーへの窓口。初めて使うときにつなぐ準備をする。
type renderService struct {
	prepared bool
	r        renderer
	err      error
}

func (s *renderService) get() (renderer, error) {
	if !s.prepared {
		s.prepared = true
		s.r, s.err = findRenderer()
	}
	return s.r, s.err
}

func (s *renderService) close() {
	if s.r != nil {
		s.r.Close()
	}
}

// imageLayer は kitty の画像表示で画面に画像を置く。
//
// 画像は id ごとに一度だけ端末へ送り、画面にはその id のプレースホルダー文字を並べる。
// 送る命令は画面の更新のあとにまとめて送る (tcell の出力と混ざらないように)。
type imageLayer struct {
	detected bool
	term     graphics.Terminal
	err      error
	shown    map[int]shownImage
	pending  []string
}

type shownImage struct {
	path       string
	cols, rows int
}

// problem は画像を出せない理由 (出せるなら nil)。初めて呼ばれたときに端末を調べる。
func (l *imageLayer) problem() error {
	if !l.detected {
		l.detected = true
		l.term, l.err = detectTerminal()
		l.shown = map[int]shownImage{}
	}
	return l.err
}

// place は area に画像 id として path を置く。端末へ送っていない絵や大きさなら、送る命令を用意する。
func (l *imageLayer) place(s tcell.Screen, area rect, id int, path string, base theme.Style) {
	if l.problem() != nil || area.w <= 0 || area.h <= 0 {
		return
	}
	image := shownImage{path, area.w, area.h}
	if l.shown[id] != image {
		l.shown[id] = image
		l.pending = append(l.pending, l.term.Show(id, path, area.w, area.h))
	}
	style := base.Cell().Foreground(tcell.PaletteColor(id))
	for row := range min(area.h, graphics.MaxRows()) {
		for col := range area.w {
			s.SetContent(area.x+col, area.y+row, graphics.Placeholder, graphics.Cell(row, col), style)
		}
	}
}

// flush は用意した命令を端末へ送る (画面を更新したあとに呼ぶ)。
func (l *imageLayer) flush(s tcell.Screen) {
	if len(l.pending) == 0 {
		return
	}
	if tty, ok := s.Tty(); ok {
		for _, command := range l.pending {
			_, _ = tty.Write([]byte(command))
		}
	}
	l.pending = l.pending[:0]
}

// close は送った画像を端末から消す。
func (l *imageLayer) close(s tcell.Screen) {
	for id := range l.shown {
		l.pending = append(l.pending, l.term.Delete(id))
	}
	l.flush(s)
}

// fitImage は幅 w・高さ h ピクセルの画像を、cols×rows マスに縦横比を保って収める大きさ。
// マスは縦長 (幅:高さ ≒ 1:2) とみなし、元の大きさより大きくはしない (1マス 8 ピクセルとして)。
func fitImage(w, h, cols, rows int) (int, int) {
	if w <= 0 || h <= 0 || cols <= 0 || rows <= 0 {
		return 0, 0
	}
	c := min(cols, max(1, (w+7)/8))
	r := max(1, (c*h+w)/(2*w))
	if r > rows {
		r = rows
		c = max(1, min(cols, (2*r*w+h/2)/h))
	}
	return c, r
}
