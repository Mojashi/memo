package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func pngBytes(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func withOpener(t *testing.T) *[]string {
	t.Helper()
	var opened []string
	old := openExternally
	openExternally = func(path string) error { opened = append(opened, path); return nil }
	t.Cleanup(func() { openExternally = old })
	return &opened
}

func TestOpeningImageShowsIt(t *testing.T) {
	withSlides(t, &fakeMarp{}, nil, nil)
	opened := withOpener(t)
	h := start(t, map[string]string{"a.md": "ノート", "photo.png": pngBytes(t, 640, 480)}, "a.md")
	h.app.openNote(filepath.Join(h.app.vault.Root, "photo.png"))
	h.app.draw()
	h.contains("photo.png  PNG  640 × 480")
	h.contains("↗ 開く")
	if h.app.images.shown[fileImageID].path == "" || !placeholderAt(h) {
		t.Fatalf("画像を置いていません:\n%s", h.screenText())
	}
	h.typeText("x") // 見ているときは文字を打っても何も起きない
	if h.app.editor.Text() != "ノート" {
		t.Errorf("ノートが書き換わりました: %q", h.app.editor.Text())
	}
	h.app.handleKey(tcell.NewEventKey(tcell.KeyRune, 'o', 0))
	if len(*opened) != 1 || filepath.Base((*opened)[0]) != "photo.png" {
		t.Errorf("既定のアプリで開いていません: %v", *opened)
	}
	h.key(tcell.KeyEsc, 0)
	h.contains("ノート")
	h.lacks("↗ 開く")
}

func TestOpenButtonClick(t *testing.T) {
	withSlides(t, &fakeMarp{}, nil, nil)
	opened := withOpener(t)
	h := start(t, map[string]string{"photo.png": pngBytes(t, 10, 10)}, "photo.png")
	b := h.app.viewer.openButton
	h.click(b.x+1, b.y)
	if len(*opened) != 1 {
		t.Errorf("ボタンで開いていません: %v", *opened)
	}
}

func TestHTMLIsShownAsPagesAndScrolls(t *testing.T) {
	withSlides(t, &fakeMarp{}, nil, nil)
	h := start(t, map[string]string{"index.html": "<h1>hi</h1>"}, "index.html")
	h.screen.SetSize(140, 40)
	h.waitFor(func() bool { return !h.app.viewer.rendering })
	h.contains("index.html  ブラウザで描いた静止画  1 / 3 画面")
	h.key(tcell.KeyDown, 0)
	h.contains("2 / 3 画面")
	if h.app.images.shown[pageImageID].path != "/cache/page-001.png" {
		t.Errorf("2枚目から並べるはず: %+v", h.app.images.shown)
	}
	h.key(tcell.KeyDown, 0)
	h.key(tcell.KeyDown, 0)
	h.contains("3 / 3 画面")
}
