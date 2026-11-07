package slides

import (
	"os"
	"os/exec"
	"testing"
)

// TestDownloadHeadlessShell は描画用の Chrome を実際に取ってくる (約 100MB なので頼まれたときだけ)。
//
//	MEMO_TEST_DOWNLOAD=1 go test ./internal/slides -run Download -v
func TestDownloadHeadlessShell(t *testing.T) {
	if os.Getenv("MEMO_TEST_DOWNLOAD") == "" {
		t.Skip("MEMO_TEST_DOWNLOAD=1 のときだけ取ってくる")
	}
	cache := t.TempDir()
	path, err := downloadShell(cache, func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", path, err, out)
	}
	t.Logf("%s", out)
	if again, ok := downloadedShell(cache); !ok || again != path {
		t.Errorf("取ってきたものを次から使うはず: %q", again)
	}

	// 取ってきた Chrome でスライドを描ける
	t.Setenv("CHROME_PATH", path)
	paths := startTestServer(t)
	client := NewClient(paths, func() error { return nil })
	defer client.Close()
	pages, err := client.Render(deck, t.TempDir(), ignore)
	if err != nil || len(pages) != 2 {
		t.Fatalf("描けません: %v %v", pages, err)
	}
	keep := os.Getenv("MEMO_TEST_KEEP")
	if keep != "" {
		data, _ := os.ReadFile(pages[0])
		_ = os.WriteFile(keep, data, 0o644)
	}
}
