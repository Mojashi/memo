package live

import (
	"strings"
	"testing"
)

func text(units []Unit) string {
	var b strings.Builder
	for _, u := range units {
		b.WriteString(u.Text)
	}
	return b.String()
}

func show(lines ...string) []string {
	blocks := Analyze(lines)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = text(Render(line, blocks[i], 40, true, plainPalette).Units)
	}
	return out
}

func TestConcealedProse(t *testing.T) {
	cases := map[string]string{
		"- [ ] 講演資料を作る **大事** な `deadline` は [ここ](https://x.y)": "☐ 講演資料を作る 大事 な deadline は ここ",
		"- [x] done":                "☑ done",
		"> 引用された*テキスト*":             "▌ 引用されたテキスト",
		"## 見出し ##":                 "見出し",
		"  - 入れ子":                   "  ◦ 入れ子",
		"1. 番号":                     "1. 番号",
		"snake_case_name と *強調*":    "snake_case_name と 強調",
		"~~消す~~ と __太い__":           "消す と 太い",
		"![図](a.png) と https://x.y": "▣ 図 と https://x.y",
		"---":                       strings.Repeat("─", 39),
		"``a`b`` c":                 "a`b c",
	}
	for in, want := range cases {
		if got := show(in)[0]; got != want {
			t.Errorf("%q\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestTableAlignsColumns(t *testing.T) {
	got := show("| 操作 | キー |", "| --- | --- |", "| 保存 | Ctrl+S |", "| x |  |")
	want := []string{
		"│ 操作 │ キー   │",
		"├──────┼────────┤",
		"│ 保存 │ Ctrl+S │",
		"│ x    │        │",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

func TestFence(t *testing.T) {
	got := show("```python", "print(1)", "```")
	want := []string{" python ", "print(1)", ""}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d got %q want %q", i, got[i], want[i])
		}
	}
}

func TestWrapAndColumns(t *testing.T) {
	line := "- [ ] あいうえお かきくけこ"
	view := Render(line, Block{}, 10, true, plainPalette)
	breaks := Wrap(view.Units, 10)
	if len(breaks) == 0 || breaks[0] != 10 {
		t.Fatalf("breaks %v", breaks)
	}
	// 2段目の先頭 x=0 は「お」の位置
	if col := ColumnAt(view.Units, breaks, 1, 0, len([]rune(line))); string([]rune(line)[col]) != "お" {
		t.Errorf("column %d", col)
	}
	// ☐ をクリック (x=0) するとマーカーの後ろ
	if col := ColumnAt(view.Units, breaks, 0, 0, len([]rune(line))); col != 6 {
		t.Errorf("marker column %d", col)
	}
	if section, x := XOf(view.Units, breaks, 8); section != 0 || x != 6 {
		t.Errorf("XOf = %d,%d", section, x)
	}
}

func TestWrapEnglishAtSpace(t *testing.T) {
	line := "hello wonderful world"
	view := Render(line, Block{}, 12, false, plainPalette)
	if got := Wrap(view.Units, 12); len(got) != 2 || got[0] != 6 || got[1] != 16 {
		t.Errorf("breaks %v", got)
	}
}

func TestNarrowTableTruncatesCells(t *testing.T) {
	lines := []string{"| 名前 | 説明 |", "| --- | --- |", "| a | とても長い説明文がここに入ります |"}
	blocks := Analyze(lines)
	for i, line := range lines {
		got := text(Render(line, blocks[i], 20, true, plainPalette).Units)
		if w := Width(got); w > 20 {
			t.Errorf("row %d too wide (%d): %q", i, w, got)
		}
		if i == 2 && !strings.Contains(got, "…") {
			t.Errorf("no ellipsis: %q", got)
		}
		t.Log(got)
	}
}

func TestMarpSlides(t *testing.T) {
	got := show(
		"---", "marp: true", "---",
		"<!-- _class: lead -->", "# 表紙",
		"---",
		"## 二枚目", "```", "---", "```",
	)
	want := []string{
		" marp ", "marp: true", "",
		"⚙ _class: lead", "表紙",
		"── スライド 2 / 2 " + strings.Repeat("─", 21),
		"二枚目", " code ", "---", "",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d 行目\n got %q\nwant %q", i, got[i], want[i])
		}
	}
	blocks := Analyze([]string{"---", "marp: true", "---", "a", "---", "b"})
	if blocks[3].Slide != 1 || blocks[5].Slide != 2 || blocks[5].Slides != 2 {
		t.Errorf("スライド番号: %+v", blocks)
	}
}

func TestFrontMatterWithoutMarpHasNoSlides(t *testing.T) {
	blocks := Analyze([]string{"---", "title: x", "---", "a", "---", "b"})
	if blocks[0].Kind != FrontMatter || blocks[4].Kind != Prose || blocks[5].Slides != 0 {
		t.Errorf("Marp でない文書: %+v", blocks)
	}
}
