package pager

import "testing"

func TestCollapseTables(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no tables",
			in:   "# Head\n\ntext\n",
			want: "# Head\n\ntext\n",
		},
		{
			name: "simple table",
			in:   "| A | B |\n| - | - |\n| 1 | 2 |\n| x | y |\n",
			want: "- A: 1\n  B: 2\n- A: x\n  B: y\n",
		},
		{
			name: "alignment colons and padded pipes",
			in:   "| Left | Center | Right |\n| :--- | :----: | ----: |\n| a | b | c |\n",
			want: "- Left: a\n  Center: b\n  Right: c\n",
		},
		{
			name: "escaped pipe in text",
			in:   "| Expr | Meaning |\n| ---- | ------- |\n| a \\| b | either |\n",
			want: "- Expr: a | b\n  Meaning: either\n",
		},
		{
			name: "escaped pipe inside code span",
			in:   "| Code | Out |\n| ---- | --- |\n| `x \\| y` | z |\n",
			want: "- Code: `x | y`\n  Out: z\n",
		},
		{
			name: "ragged row truncated to header count",
			in:   "| A | B |\n| - | - |\n| 1 | 2 | 3 |\n| only |\n",
			want: "- A: 1\n  B: 2\n- A: only\n",
		},
		{
			name: "empty cells skipped",
			in:   "| A | B | C |\n| - | - | - |\n| 1 |   | 3 |\n|   | 2 |   |\n",
			want: "- A: 1\n  C: 3\n- B: 2\n",
		},
		{
			name: "all-empty row dropped",
			in:   "| A |\n| - |\n|   |\n| x |\n",
			want: "- A: x\n",
		},
		{
			name: "link keeps url",
			in:   "| Name | Site |\n| ---- | ---- |\n| [glamour](https://example.com/g) | plain |\n",
			want: "- Name: [glamour](https://example.com/g)\n  Site: plain\n",
		},
		{
			name: "inline markup preserved",
			in:   "| Fmt | Val |\n| --- | --- |\n| **bold** *it* ~~s~~ `c` | v |\n",
			want: "- Fmt: **bold** *it* ~~s~~ `c`\n  Val: v\n",
		},
		{
			name: "multiple tables with prose between",
			in:   "intro\n\n| A |\n| - |\n| 1 |\n\nmid\n\n| B |\n| - |\n| 2 |\n\ntail\n",
			want: "intro\n\n- A: 1\n\nmid\n\n- B: 2\n\ntail\n",
		},
		{
			name: "table after paragraph on same buffer start",
			in:   "| H |\n| - |\n| v |\n",
			want: "- H: v\n",
		},
		{
			name: "empty header gets placeholder",
			in:   "|   | B |\n| - | - |\n| 1 | 2 |\n",
			want: "- col 1: 1\n  B: 2\n",
		},
		{
			name: "header-only table",
			in:   "| A | B |\n| - | - |\n",
			want: "",
		},
		{
			name: "header-only table with prose around",
			in:   "before\n\n| A |\n| - |\n\nafter\n",
			want: "before\n\n\nafter\n",
		},
		{
			name: "table inside blockquote",
			in:   "> | A | B |\n> | - | - |\n> | 1 | 2 |\n",
			want: "- A: 1\n  B: 2\n",
		},
		{
			name: "header-only table inside blockquote",
			in:   "> | A |\n> | - |\n",
			want: "",
		},
		{
			name: "table inside list item",
			in:   "- item\n\n  | A |\n  | - |\n  | 1 |\n",
			want: "- item\n\n- A: 1\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := collapseTables(tc.in); got != tc.want {
				t.Errorf("collapse:\nin:  %q\ngot: %q\nwant: %q", tc.in, got, tc.want)
			}
		})
	}
}
