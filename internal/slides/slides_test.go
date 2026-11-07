package slides

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const deck = "---\nmarp: true\n---\n\n# 一枚目\n\n---\n\n## 二枚目\n"

// startTestServer は描画サーバーをこのプロセスの中で動かす。テストが終わると Chrome ごと止まる。
func startTestServer(t *testing.T) Paths {
	t.Helper()
	found := false
	for _, name := range installedChromes[runtime.GOOS] {
		if _, err := exec.LookPath(name); err == nil {
			found = true
		}
	}
	if !found {
		t.Skip("Chrome が入っていない (このテストのために取ってくるのは重い)")
	}
	dir, err := os.MkdirTemp("", "ms") // Unix ソケットのパスは短くないといけない
	if err != nil {
		t.Fatal(err)
	}
	paths := Paths{dir}
	done := make(chan error, 1)
	go func() { done <- serve(paths, 200*time.Millisecond) }()
	t.Cleanup(func() {
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("誰もつながっていないのに描画サーバーが終わりません")
		}
		os.RemoveAll(dir)
	})
	return paths
}

func ignore(string) {}

func TestSharedServerRendersOnlyChangedSlides(t *testing.T) {
	paths := startTestServer(t)
	first := NewClient(paths, func() error { return nil }) // 描画サーバーは startTestServer で起動済み
	defer first.Close()
	pages, err := first.Render(deck, t.TempDir(), ignore)
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

	// 別の memo からつないでも同じ描画サーバーを使い、同じスライドは同じ画像になる
	second := NewClient(paths, func() error { t.Fatal("描画サーバーを2つ起動しようとしました"); return nil })
	defer second.Close()
	changed, err := second.Render("---\nmarp: true\n---\n\n# 一枚目\n\n---\n\n## 変えた\n", os.TempDir(), ignore)
	if err != nil {
		t.Fatal(err)
	}
	if changed[1] == pages[1] {
		t.Errorf("変えたスライドは別の画像になるはず: %v %v", pages, changed)
	}
	again, err := first.Render(deck, os.TempDir(), ignore)
	if err != nil {
		t.Fatal(err)
	}
	if again[0] != changed[0] {
		t.Errorf("同じスライドは同じ画像のはず: %v %v", again, changed)
	}
}

func TestSecondServerStepsAside(t *testing.T) {
	paths := startTestServer(t)
	time.Sleep(50 * time.Millisecond) // 1つ目がロックを取るのを待つ
	if err := serve(paths, time.Second); err != ErrServerRunning {
		t.Errorf("2つ目の描画サーバーは身を引くはず: %v", err)
	}
}

func TestPageIsCutIntoScreens(t *testing.T) {
	paths := startTestServer(t)
	client := NewClient(paths, func() error { return nil })
	defer client.Close()
	dir := t.TempDir()
	page := filepath.Join(dir, "page.html")
	if err := os.WriteFile(page, []byte(`<!doctype html><body style="margin:0"><h1>見出し</h1><div style="height:2000px;background:linear-gradient(red,blue)"></div></body>`), 0o644); err != nil {
		t.Fatal(err)
	}
	chunks, err := client.RenderPage(page, ignore)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 { // 2000px 余りを 720px ずつ
		t.Fatalf("3枚に切るはず: %v", chunks)
	}
	again, err := client.RenderPage(page, ignore)
	if err != nil || again[0] != chunks[0] {
		t.Errorf("変わっていなければ撮り直さない: %v %v", again, err)
	}
}
