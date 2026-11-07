package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/Mojashi/memo/internal/graphics"
	"github.com/Mojashi/memo/internal/slides"
	"github.com/Mojashi/memo/internal/textbuf"
)

// fakeMarp はスライドの枚数ぶんの画像のパスを返す (ファイルは書かない)。
type fakeMarp struct{ calls int }

func (f *fakeMarp) Render(source, dir, out string) ([]string, error) {
	f.calls++
	pages := strings.Count(source, "\n---\n")
	var paths []string
	for i := range pages {
		paths = append(paths, fmt.Sprintf("%s/slide.%03d.png", out, i+1))
	}
	return paths, nil
}

func withSlides(t *testing.T, marp slideRenderer, marpErr, termErr error) {
	t.Helper()
	oldFind, oldDetect := findMarp, detectTerminal
	findMarp = func() (slideRenderer, error) { return marp, marpErr }
	detectTerminal = func() (graphics.Terminal, error) { return graphics.Terminal{}, termErr }
	t.Cleanup(func() { findMarp, detectTerminal = oldFind, oldDetect })
}

// waitSlides は裏で描いているスライドが終わるまで、メインループに届く処理を実行する。
func (h *harness) waitSlides() {
	for h.app.slides.rendering {
		if call, ok := h.screen.PollEvent().(*callEvent); ok {
			call.fn()
		}
	}
	h.app.draw()
}

func placeholderAt(h *harness) bool {
	cells, w, _ := h.screen.GetContents()
	for _, c := range cells[:w*10] {
		if len(c.Runes) > 0 && c.Runes[0] == graphics.Placeholder {
			return true
		}
	}
	return false
}

const deck = "---\nmarp: true\n---\n\n# 表紙\n\n---\n\n## 二枚目\n"

func TestMarpShowsCurrentSlideImage(t *testing.T) {
	marp := &fakeMarp{}
	withSlides(t, marp, nil, nil)
	h := start(t, map[string]string{"deck.md": deck}, "deck.md")
	h.screen.SetSize(140, 40)
	h.waitSlides()
	h.contains("▶ スライド 1 / 2")
	h.contains("── スライド 2 / 2")
	if !placeholderAt(h) {
		t.Fatalf("スライドの画像を置いていません:\n%s", h.screenText())
	}
	if !strings.HasSuffix(h.app.slides.shown.path, "slide.001.png") {
		t.Errorf("1枚目を出すはず: %q", h.app.slides.shown.path)
	}
	h.app.editor.MoveTo(textbuf.Pos{Row: 8, Col: 0}, false)
	h.app.draw()
	h.contains("▶ スライド 2 / 2")
	if !strings.HasSuffix(h.app.slides.shown.path, "slide.002.png") {
		t.Errorf("2枚目を出すはず: %q", h.app.slides.shown.path)
	}
	h.key(tcell.KeyF7, 0)
	h.lacks("▶ スライド")
}

func TestMarpWithoutCLIExplainsHowToInstall(t *testing.T) {
	withSlides(t, nil, slides.ErrNoMarp, nil)
	h := start(t, map[string]string{"deck.md": deck}, "deck.md")
	h.screen.SetSize(140, 40)
	h.app.draw()
	h.contains("npm i -g @marp-team/marp-cli")
}

func TestPlainMarkdownHasNoSlidePane(t *testing.T) {
	marp := &fakeMarp{}
	withSlides(t, marp, nil, nil)
	h := start(t, map[string]string{"a.md": "---\ntitle: x\n---\n\n本文\n"}, "a.md")
	h.screen.SetSize(140, 40)
	h.app.draw()
	h.lacks("▶ スライド")
	if marp.calls != 0 {
		t.Errorf("Marp でない文書を描きに行っています")
	}
}
