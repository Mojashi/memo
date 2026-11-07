# memo

ターミナルで書く、リッチな Markdown ノートエディタ。

![memo のスクリーンショット](docs/screenshot.png)

- **ライブプレビュー**: カーソルのある行だけ Markdown の記号を見せ、ほかの行は完成形で描きます
  (見出し・☐/☑ チェックリスト・箇条書き・引用・表・コードブロック・太字/斜体/リンク)
- **表は罫線つきで桁を揃えます**。日本語の全角幅にも対応し、画面に収まらないセルは「…」で切ります
- **Mermaid の図をその場で描きます** (flowchart / graph・sequenceDiagram・erDiagram。描画は [mermaid-ascii](https://github.com/AlexanderGrooff/mermaid-ascii))。
  コードブロックにカーソルを入れるとソースに戻ります
- **Marp のスライド**: front matter に `marp: true` があると、`---` を「スライド 2 / 5」の区切りとして描き、
  `<!-- _class: lead -->` のような指示は ⚙ で控えめに見せます。
  カーソルのあるスライドを [Marp](https://marp.app) の**本物の見た目の画像で右側に出します** (kitty / Ghostty。F7 で表示切替)
- **画像 (PNG / JPEG / GIF / WebP / BMP) と HTML・SVG をその場で見られます**。HTML は Chrome で描いた静止画で、
  縦に長いページは ↑↓ でめくります。「↗ 開く」(か `o`) でブラウザや既定のアプリで開きます (kitty / Ghostty)
- ファイル一覧・目次・全文検索・コマンドパレット・自動保存・テーマ
- Markdown 以外のテキストファイルも行番号つき・色付きで開けます
- マウス対応 (クリック・ドラッグ選択・☐ のクリック・ツールバー)

## インストール

[Releases](https://github.com/Mojashi/memo/releases) から自分の OS 用のバイナリを取って PATH に置くか、Go (1.25 以上) があれば:

```sh
go install github.com/Mojashi/memo@latest
```

## 使い方

```sh
memo            # 今のフォルダを開く
memo notes/     # フォルダを開く
memo todo.md    # ファイルを開く (親フォルダが一覧に出る)
```

| キー | 操作 |
| --- | --- |
| Ctrl+S / Ctrl+N / Ctrl+O | 保存 / 新しいノート / ファイルを開く |
| Ctrl+F | 全ノートを検索 (ファイル名と本文) |
| Ctrl+P | コマンドパレット (テーマ変更もここから) |
| Ctrl+E | サイドバーへ移動 / 戻る (サイドバーで Backspace か `..` で親フォルダへ) |
| F2 / F4 / F6 | サイドバー / 集中モード (Esc でも戻る) / 記号を全部見せる |
| F7 | Marp のスライドの画像を出す / しまう |
| Ctrl+B / Ctrl+L / Ctrl+T | 太字 / リンク / チェックの切り替え |
| Ctrl+Z / Ctrl+Y | 元に戻す / やり直す |
| Option+← → (Ctrl+← →) | 単語ごとに移動 (Shift で選択) |
| Tab / Shift+Tab | 字下げ / 字下げを戻す |
| Ctrl+Q | 終了 (名前のあるノートは保存してから) |

Enter でリスト・チェックリスト・番号・引用が続きます。空の項目で Enter すると記号が消えます。

### Marp のスライド・画像・HTML を見る

追加で入れるものはありません (Node や marp-cli も不要)。

- スライドは memo に埋め込んだ [marp-core](https://github.com/marp-team/marp-core) を Chrome で動かして描きます。
  Chrome (Chromium / Edge / Brave) が入っていればそれを使い、無ければ描画用の
  [chrome-headless-shell](https://developer.chrome.com/blog/chrome-headless-shell) (約 100MB) を初回に自動で取ってきます
- HTML・SVG のファイルも同じ仕組み (Chrome) で描きます
- 描画は裏で動く描画サーバー (`memo --slide-server`) が受け持ち、memo をいくつ開いても1つを共有します。
  Chrome を起動したまま使い回し、変わったスライドだけを描き直します (最初の1回は約1秒、その後は 0.1 秒ほど)。
  誰もつながっていない状態が5分続くと終わります
- 置き場は `~/Library/Caches/memo/slides` (Linux は `~/.cache/memo/slides`)。`CHROME_PATH` で使う Chrome を指定できます

画像は kitty の画像表示 (Unicode プレースホルダー) で出すので、kitty か Ghostty で使えます。
tmux の中で使うときは `~/.tmux.conf` に次の1行が要ります。

```tmux
set -g allow-passthrough on
```

## 開発

```sh
go test ./...
go run .
# README のスクリーンショットを撮り直す
MEMO_SCREENSHOT=$PWD/docs/screenshot.svg go test ./internal/ui -run Screenshot -count=1
rsvg-convert -z 2 docs/screenshot.svg -o docs/screenshot.png
# 埋め込みの marp-core を作り直す (internal/slides/marp.js)
cd tools/marp-bundle && npm install && npm run build
```
