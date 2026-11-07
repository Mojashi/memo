package graphics

import (
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadImageConvertsToPNG(t *testing.T) {
	dir := t.TempDir()
	photo := filepath.Join(dir, "photo.JPG")
	f, err := os.Create(photo)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 320, 200)), nil); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if !IsImage(photo) {
		t.Fatal("JPG は画像として開くはず")
	}
	file, err := LoadImage(photo, filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	if file.Width != 320 || file.Height != 200 || file.Format != "jpeg" || filepath.Ext(file.PNG) != ".png" {
		t.Errorf("読み取り: %+v", file)
	}
	if _, err := os.Stat(file.PNG); err != nil {
		t.Errorf("PNG に変換していません: %v", err)
	}
	if _, err := LoadImage(filepath.Join(dir, "missing.png"), dir); err == nil {
		t.Error("無いファイルは失敗するはず")
	}
}

func TestPlaceholderCellsNumberOnlyTheFirstColumn(t *testing.T) {
	if got := Cell(2, 0); len(got) != 2 || got[0] != diacritics[2] || got[1] != diacritics[0] {
		t.Errorf("行頭は行と列の番号: %U", got)
	}
	if Cell(2, 5) != nil {
		t.Error("2マス目以降は左のマスから続きと分かる")
	}
}
