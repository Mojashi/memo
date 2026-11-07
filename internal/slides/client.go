package slides

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	// startTimeout は描画サーバーを起動してから、つながるまで待つ上限。
	startTimeout = 10 * time.Second
	// replyTimeout は1回の描画を待つ上限 (初回は Chrome を取ってくる時間を含む)。
	replyTimeout = 5 * time.Minute
)

// Client は描画サーバーにつなぐ memo 側。描画サーバーが動いていなければ起動する。
type Client struct {
	paths Paths
	start func() error // 描画サーバーを起動する

	mu     sync.Mutex // スライドと HTML の依頼が重ならないように
	conn   net.Conn
	reader *bufio.Scanner
}

// Connect は描画サーバーへの窓口を用意する (つなぐのは最初の Render のとき)。
// 描画サーバーは、この実行ファイルを `--slide-server` で起動したもの。
func Connect() (*Client, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("memo の場所が分かりません: %w", err)
	}
	return NewClient(paths, func() error { return startServer(self, paths) }), nil
}

// NewClient は start で描画サーバーを起動する窓口 (テストで別の起動方法を渡す)。
func NewClient(paths Paths, start func() error) *Client {
	return &Client{paths: paths, start: start}
}

// ServerFlag は memo を描画サーバーとして起動する引数。
const ServerFlag = "--slide-server"

func startServer(self string, paths Paths) error {
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return err
	}
	log, err := os.OpenFile(paths.Log(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(self, ServerFlag)
	cmd.Stdout, cmd.Stderr = log, log
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("描画サーバーを起動できません: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// Render は Markdown のソースを1枚ずつの PNG にし、そのパスをスライド順に返す。
// 途中経過 (Chrome を取ってきているなど) は status に知らせる。
func (c *Client) Render(source, dir string, status func(string)) ([]string, error) {
	return c.ask(request{Markdown: source, Dir: dir}, status)
}

// RenderPage は HTML (や SVG) のファイルを静止画にし、上から 1280×720 ずつに切った PNG のパスを返す。
func (c *Client) RenderPage(file string, status func(string)) ([]string, error) {
	return c.ask(request{Page: file}, status)
}

func (c *Client) ask(req request, status func(string)) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.connect(); err != nil {
		return nil, err
	}
	pages, err := c.exchange(req, status)
	var failed *RenderError
	if err != nil && !errors.As(err, &failed) {
		c.closeLocked() // つながりが壊れた。次はつなぎ直す
	}
	return pages, err
}

func (c *Client) exchange(req request, status func(string)) ([]string, error) {
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	_ = c.conn.SetDeadline(time.Now().Add(replyTimeout))
	if _, err := c.conn.Write(append(line, '\n')); err != nil {
		return nil, fmt.Errorf("描画サーバーに送れません: %w", err)
	}
	for c.reader.Scan() {
		var r reply
		if err := json.Unmarshal(c.reader.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("描画サーバーの返事を読めません: %w", err)
		}
		if !r.final() {
			status(r.Status)
			continue
		}
		if r.Error != "" {
			return nil, &RenderError{r.Error}
		}
		return r.Pages, nil
	}
	if err := c.reader.Err(); err != nil {
		return nil, fmt.Errorf("描画サーバーから返事がありません: %w", err)
	}
	return nil, errors.New("描画サーバーが止まりました")
}

// RenderError は描画サーバーが描けなかったとき (つながりは保つ)。
type RenderError struct{ Message string }

func (e *RenderError) Error() string { return e.Message }

// connect は描画サーバーにつなぐ。動いていなければ起動して、つながるまで待つ。
func (c *Client) connect() error {
	if c.conn != nil {
		return nil
	}
	conn, err := net.Dial("unix", c.paths.Socket())
	if err != nil {
		if err := c.start(); err != nil {
			return err
		}
		deadline := time.Now().Add(startTimeout)
		for {
			if conn, err = net.Dial("unix", c.paths.Socket()); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("描画サーバーにつながりません (記録: %s): %w", c.paths.Log(), err)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	c.conn = conn
	c.reader = bufio.NewScanner(conn)
	c.reader.Buffer(make([]byte, 1<<16), 1<<24)
	return nil
}

// Close は描画サーバーとのつながりを切る (描画サーバーは他の memo のために残る)。
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked()
}

func (c *Client) closeLocked() {
	if c.conn != nil {
		c.conn.Close()
		c.conn, c.reader = nil, nil
	}
}
