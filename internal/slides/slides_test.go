package slides

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderWritesOnePNGPerSlide(t *testing.T) {
	marp, err := Find()
	if err != nil {
		t.Skip(err) // CI には marp-cli が無い
	}
	dir := t.TempDir()
	pages, err := marp.Render("---\nmarp: true\n---\n\n# 一枚目\n\n---\n\n## 二枚目\n", dir, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("2枚のはず: %v", pages)
	}
	for _, page := range pages {
		if info, err := os.Stat(page); err != nil || info.Size() == 0 {
			t.Errorf("画像がありません: %s %v", page, err)
		}
	}
}
