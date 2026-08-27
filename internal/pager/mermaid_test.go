package pager

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestLayoutFlowGoldens(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"td straight arrow", "flowchart TD\nA[Start] --> B[End]",
			"┌─────┐\n│Start│\n└──┬──┘\n   │\n   ▼\n ┌─┴─┐\n │End│\n └───┘"},
		{"td chained shapes and wrap", "flowchart TB\nA[One two three four five six seven eight nine ten eleven twelve thirteen fourteen] --> B{Decision} --> C(Round)",
			"┌────────────────┐\n│ One two three  │\n│ four five six  │\n│seven eight nine│\n│   ten eleven   │\n│twelve thirteen │\n│    fourteen    │\n└────────┬───────┘\n         │\n         ▼\n    ┌────┴───┐\n    │Decision│\n    └────┬───┘\n        ┌┘\n        ▼\n     ┌──┴──┐\n     │Round│\n     └─────┘"},
		{"td open link", "flowchart TD\nA --- B",
			"┌─┐\n│A│\n└┬┘\n │\n ▼\n┌┴┐\n│B│\n└─┘"},
		{"td labeled fork", "flowchart TD\nA -->|yes| B\nA -->|no| C",
			"   ┌─┐\n   │A│\n   └┬┘\n ┌──┴───┐\n │yesno │\n ▼      ▼\n┌┴┐    ┌┴┐\n│B│    │C│\n└─┘    └─┘"},
		{"td skip edge detour", "flowchart TD\nA[Begin] --> B[Middle]\nB --> C[Final]\nA --> C",
			" ┌─────┐\n │Begin│\n └──┬──┘\n┌───┴┐\n│    ▼\n│┌───┴──┐\n││Middle│\n│└───┬──┘\n└───┬┘\n    ▼\n ┌──┴──┐\n │Final│\n └─────┘"},
		{"lr chain", "flowchart LR\nA --> B --> C",
			"┌─┐     ┌─┐     ┌─┐\n│A├────▶┤B├────▶┤C│\n└─┘     └─┘     └─┘"},
		{"lr edge label", "flowchart LR\nA[Left side] -->|tagged| B --> C",
			"┌─────────┐  tagged  ┌─┐     ┌─┐\n│Left side├─────────▶┤B├────▶┤C│\n└─────────┘          └─┘     └─┘"},
		{"td cjk label", "flowchart TD\nA[中文] --> B[ok]",
			"┌────┐\n│中文│\n└──┬─┘\n   │\n   ▼\n ┌─┴┐\n │ok│\n └──┘"},
		{"td cylinder", "flowchart TD\nA --> db[(Database)]",
			"   ┌─┐\n   │A│\n   └┬┘\n    └┐\n     ▼\n╭────┴───╮\n│Database│\n╰────────╯"},
		{"lr cylinder", "flowchart LR\napi --> db[(Database)]",
			"┌───┐     ╭────────╮\n│api├────▶┤Database│\n└───┘     ╰────────╯"},
		{"td mixed shapes with edge label", "flowchart TD\nA[Box] --> db[(Database)]\nA -->|go| C(Round)",
			"        ┌───┐\n        │Box│\n        └─┬─┘\n     ┌────┴──────┐\n     │      go   │\n     ▼           ▼\n╭────┴───╮    ┌──┴──┐\n│Database│    │Round│\n╰────────╯    └─────┘"},
		{"cycle drops closing edge", "flowchart TD\nA --> B\nB --> A",
			"┌─┐\n│A│\n└┬┘\n │\n ▼\n┌┴┐\n│B│\n└─┘"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := layoutMermaid(tc.in)
			if !ok {
				t.Fatalf("unexpected decline:\n%s", tc.in)
			}
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// The exact flowchart from example.md must render end-to-end; its queue --> api
// edge closes a cycle and is omitted from the drawing as the back edge.
func TestExampleDocFlowchartGolden(t *testing.T) {
	in := "flowchart TD\n" +
		"    user[User] --> lb[Load Balancer]\n" +
		"    lb --> api[API Server]\n" +
		"    lb --> ws[WebSocket GW]\n" +
		"    api --> db[(Database)]\n" +
		"    api --> cache[Cache]\n" +
		"    api -->|gRPC| worker[Worker Pool]\n" +
		"    worker --> queue[Queue] --> api\n"
	want := `
                ┌────┐
                │User│
                └──┬─┘
                  ┌┘
                  ▼
           ┌──────┴──────┐
           │Load Balancer│
           └──────┬──────┘
          ┌───────┴────────┐
          ▼                ▼
    ┌─────┴────┐    ┌──────┴─────┐
    │API Server│    │WebSocket GW│
    └─────┬────┘    └────────────┘
     ┌────┴──────┬─────────────┐
     │           │gRPC         │
     ▼           ▼             ▼
╭────┴───╮    ┌──┴──┐    ┌─────┴─────┐
│Database│    │Cache│    │Worker Pool│
╰────────╯    └─────┘    └─────┬─────┘
                  ┌────────────┘
                  ▼
               ┌──┴──┐
               │Queue│
               └─────┘`[1:]
	got, ok := layoutMermaid(in)
	if !ok {
		t.Fatalf("unexpected decline:\n%s", in)
	}
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSequenceGolden(t *testing.T) {
	in := "sequenceDiagram\nparticipant A as Alice\nparticipant B\nautonumber\nA->>B: hello there\nB-->>A: hi back\nNote over A,B: the note text"
	want := "┌─────┐      ┌─┐\n│Alice│      │B│\n└─────┘      └─┘\n   hello there│\n   ┼──────────▶\n   │          │\n   │ hi back  │\n   ┼──────────◁\n   │          │\n ┌─────────────┐\n │the note text│\n └─────────────┘\n   │          │"
	got, ok := layoutSequence(strings.Split(in, "\n")[1:])
	if !ok {
		t.Fatal("unexpected decline")
	}
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMermaidDeclineMatrix(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"subgraph", "flowchart TD\nA --> B\nsubgraph X\nC --> D\nend"},
		{"classDef", "flowchart TD\nclassDef red fill:#f00\nA --> B"},
		{"style", "flowchart TD\nA --> B\nstyle A fill:#f9f"},
		{"click", "flowchart TD\nA --> B\nclick A href \"https://example.com\""},
		{"dotted arrow", "flowchart TD\nA -.-> B"},
		{"thick arrow", "flowchart TD\nA ==> B"},
		{"graph keyword", "graph TD\nA --> B"},
		{"bt direction", "flowchart BT\nA --> B"},
		{"self loop", "flowchart TD\nA --> A"},
		{"empty cylinder", "flowchart TD\nA[( )] --> B"},
		{"unclosed bracket", "flowchart TD\nA[B --> B"},
		{"double circle shape", "flowchart TD\nA((start)) --> B"},
		{"alt edge label form", "flowchart TD\nA --text--- B"},
		{"empty diagram", ""},
		{"seq loop block", "sequenceDiagram\nloop every day\nA->>B: hi\nend"},
		{"seq activation", "sequenceDiagram\nA->>+B: act\nB-->>-A: done"},
		{"seq self message", "sequenceDiagram\nA->>A: self"},
		{"seq note left", "sequenceDiagram\nNote left of A: text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := "```mermaid\n" + tc.body + "\n```\n"
			if got := expandMermaid(doc); got != doc {
				t.Errorf("declined diagram must stay byte-exact\ngot:  %q\nwant: %q", got, doc)
			}
		})
	}
}

func TestExpandMermaidSplice(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain doc", "# Doc\n\n```mermaid\nflowchart TD\nA --> B\n```\n\ntail\n",
			"# Doc\n\n```mermaid\n┌─┐\n│A│\n└┬┘\n │\n ▼\n┌┴┐\n│B│\n└─┘\n```\n\ntail\n"},
		{"blockquote prefix", "> ```mermaid\n> flowchart LR\n> A --> B\n> ```\n",
			"> ```mermaid\n> ┌─┐     ┌─┐\n> │A├────▶┤B│\n> └─┘     └─┘\n> ```\n"},
		{"list indent", "- item\n\n  ```mermaid\n  flowchart LR\n  A --> B\n  ```\n",
			"- item\n\n  ```mermaid\n  ┌─┐     ┌─┐\n  │A├────▶┤B│\n  └─┘     └─┘\n  ```\n"},
		{"unclosed at eof", "```mermaid\nflowchart TD\nA --> B",
			"```mermaid\n┌─┐\n│A│\n└┬┘\n │\n ▼\n┌┴┐\n│B│\n└─┘\n```\n\n"},
		{"non-mermaid fence untouched", "```go\nflowchart TD\nA --> B\n```\n",
			"```go\nflowchart TD\nA --> B\n```\n"},
		{"adjacent mermaid and code fence", "```mermaid\nflowchart TD\nA --> B\n```\n```go\nx := 1\n```\n",
			"```mermaid\n┌─┐\n│A│\n└┬┘\n │\n ▼\n┌┴┐\n│B│\n└─┘\n```\n```go\nx := 1\n```\n"},
		{"adjacent mermaid blocks", "```mermaid\nflowchart TD\nA --> B\n```\n```mermaid\nflowchart LR\nC --> D\n```\n",
			"```mermaid\n┌─┐\n│A│\n└┬┘\n │\n ▼\n┌┴┐\n│B│\n└─┘\n```\n```mermaid\n┌─┐     ┌─┐\n│C├────▶┤D│\n└─┘     └─┘\n```\n"},
		{"code fence survives after mermaid", "# Doc\n\n```mermaid\nflowchart LR\nA --> B\n```\n\n```go\nfmt.Println()\n```\n",
			"# Doc\n\n```mermaid\n┌─┐     ┌─┐\n│A├────▶┤B│\n└─┘     └─┘\n```\n\n```go\nfmt.Println()\n```\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := expandMermaid(tc.in); got != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func TestDiagramWidthSanity(t *testing.T) {
	for _, in := range []string{
		"flowchart TD\nA[Start] --> B[End]\nA -->|yes| B",
		"flowchart LR\nA -->|tag| B --> C",
		"flowchart TD\nA --> db[(Database)]",
		"flowchart TD\nA --> B\nB --> A",
		"sequenceDiagram\nA->>B: msg\nNote over A,B: note",
	} {
		out, ok := layoutMermaid(in)
		if !ok {
			t.Fatalf("%q: unexpected decline", in)
		}
		for i, l := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(l); w != len([]rune(l)) {
				t.Fatalf("%q: line %d width %d != %d runes: %q", in, i, w, len([]rune(l)), l)
			}
		}
	}
}

func TestRenderIntegratesDiagramAndMath(t *testing.T) {
	doc := "# T\n\n$E=mc^2$ prose\n\n```mermaid\nflowchart TD\nA --> B\n```\n"
	out, err := Render(doc, 60)
	if err != nil {
		t.Fatal(err)
	}
	s := ansi.Strip(out)
	if !strings.Contains(s, "E=mc²") {
		t.Errorf("math not substituted: %q", s)
	}
	if !strings.Contains(s, "┌─┐") || !strings.Contains(s, "▼") {
		t.Errorf("diagram missing: %q", s)
	}
}
