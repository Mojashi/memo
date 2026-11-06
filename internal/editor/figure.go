package editor

import (
	"slices"
	"strings"
	"time"

	"github.com/Mojashi/memo/internal/diagram"
	"github.com/Mojashi/memo/internal/live"
)

// figureWait は図の描画をその場で待つ時間。超えたら描けた時点で画面を更新する。
const figureWait = 150 * time.Millisecond

type figureKey struct {
	source string
	width  int
}

type figure struct {
	lines []string
	err   error
	ready bool
}

// figureCache は描いた図。同じソース・幅なら描き直さない。
type figureCache map[figureKey]*figure

// diagramSource は Mermaid のコードブロックの中身 (開始行と終了行を除く)。
func (e *Editor) diagramSource(block live.Block) string {
	lines := e.buf.Lines()
	end := block.Close
	if e.blockOf(block.Close).Kind != live.FenceClose {
		end++ // 閉じていないブロックは最終行まで中身
	}
	return strings.Join(lines[block.Open+1:end], "\n")
}

// figureLayout は図として描く行の見え方。図にできなければ nil (普通のコードブロックとして描く)。
func (e *Editor) figureLayout(i int, text string, block live.Block, key layoutKey) *layout {
	fig := e.figure(key.diagram)
	if fig.err != nil {
		return nil
	}
	if block.Kind != live.FenceOpen {
		return &layout{hidden: true}
	}
	view := live.Render(text, block, e.textW, true, e.palette)
	l := &layout{units: view.Units, fill: view.Fill, figureSource: min(block.Open+1, block.Close), figureEnd: block.Close}
	l.breaks = live.Wrap(view.Units, e.textW)
	if fig.ready {
		l.figure = append(slices.Clone(fig.lines), "")
	} else {
		l.figure = []string{"図を描いています…"}
	}
	return l
}

// figure は Mermaid のソースを図にする。時間がかかれば描き終わりを待たずに返し、終わったら画面を更新する。
func (e *Editor) figure(source string) *figure {
	key := figureKey{source, e.textW}
	if e.figures == nil {
		e.figures = figureCache{}
	}
	if fig, ok := e.figures[key]; ok {
		return fig
	}
	if len(e.figures) > 256 {
		clear(e.figures)
	}
	done := make(chan *figure, 1)
	go func() {
		lines, err := diagram.Render(source, key.width-1)
		done <- &figure{lines: lines, err: err, ready: true}
	}()
	select {
	case fig := <-done:
		e.figures[key] = fig
		return fig
	case <-time.After(figureWait):
	}
	pending := &figure{}
	e.figures[key] = pending
	if e.Post != nil {
		go func() {
			fig := <-done
			e.Post(func() {
				e.figures[key] = fig
				e.invalidate()
			})
		}()
	}
	return pending
}
