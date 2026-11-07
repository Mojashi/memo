package graphics

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"

	_ "image/gif"  // GIF を読む
	_ "image/jpeg" // JPEG を読む

	_ "golang.org/x/image/bmp"  // BMP を読む
	_ "golang.org/x/image/webp" // WebP を読む
)

// imageSuffixes は画像として開くファイル。
var imageSuffixes = []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"}

// IsImage は画像として開くファイルか。
func IsImage(path string) bool {
	return slices.Contains(imageSuffixes, strings.ToLower(filepath.Ext(path)))
}

// ImageFile は端末に送れる形にした画像。
type ImageFile struct {
	PNG           string // 端末に送る PNG (PNG 以外は cache に変換したもの)
	Width, Height int    // ピクセル
	Bytes         int64  // 元のファイルの大きさ
	Format        string // png, jpeg, gif, …
}

// LoadImage は画像の大きさを調べ、PNG でなければ cache に PNG を書く (kitty が直接読めるのは PNG だけ)。
func LoadImage(path, cache string) (ImageFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return ImageFile{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return ImageFile{}, err
	}
	defer f.Close()
	config, format, err := image.DecodeConfig(f)
	if err != nil {
		return ImageFile{}, fmt.Errorf("%s は画像として読めません: %w", filepath.Base(path), err)
	}
	file := ImageFile{PNG: path, Width: config.Width, Height: config.Height, Bytes: info.Size(), Format: format}
	if format == "png" {
		return file, nil
	}
	sum := sha1.Sum(fmt.Appendf(nil, "%s\x00%d\x00%d", path, info.Size(), info.ModTime().UnixNano()))
	file.PNG = filepath.Join(cache, hex.EncodeToString(sum[:10])+".png")
	if _, err := os.Stat(file.PNG); err == nil {
		return file, nil
	}
	if _, err := f.Seek(0, 0); err != nil {
		return ImageFile{}, err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return ImageFile{}, fmt.Errorf("%s は画像として読めません: %w", filepath.Base(path), err)
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return ImageFile{}, err
	}
	out, err := os.CreateTemp(cache, ".tmp-*.png")
	if err != nil {
		return ImageFile{}, err
	}
	defer os.Remove(out.Name())
	if err := png.Encode(out, img); err != nil {
		out.Close()
		return ImageFile{}, err
	}
	if err := out.Close(); err != nil {
		return ImageFile{}, err
	}
	return file, os.Rename(out.Name(), file.PNG)
}
