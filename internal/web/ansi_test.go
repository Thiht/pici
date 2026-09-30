package web

import (
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/Thiht/pici/internal/stores"
)

func TestAnsiToHTML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello world", "hello world"},
		{"red", "\x1b[31mred\x1b[0m", `<span class="term-fg31">red</span>`},
		{"bold-green", "\x1b[1;32mok\x1b[0m", `<span class="term-fg32 term-fg1">ok</span>`},
		{"bright-blue", "\x1b[94mhi\x1b[0m", `<span class="term-fgi94">hi</span>`},
		{"background", "\x1b[41mwarn\x1b[0m", `<span class="term-bg41">warn</span>`},
		{"reset-in-middle", "\x1b[31ma\x1b[0mb", `<span class="term-fg31">a</span>b`},
		{"256-color", "\x1b[38;5;196mX\x1b[0m", `<span class="term-fgx196">X</span>`},
		// terminal-to-html has no class for 24-bit colours, so truecolor styling
		// degrades to a span with no colour (the text is still preserved).
		{"truecolor-unstyled", "\x1b[38;2;1;2;3mX\x1b[0m", `<span class="">X</span>`},
		{"non-sgr-dropped", "\x1b[2Kclean", "clean"},
		{"escaped-html", "<script>alert(1)</script>", "&lt;script&gt;alert(1)&lt;&#47;script&gt;"},
		{"escaped-html-in-color", "\x1b[31m<b>\x1b[0m", `<span class="term-fg31">&lt;b&gt;</span>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(ansiToHTML(tc.in)); got != tc.want {
				t.Errorf("ansiToHTML(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAnsiDoesNotEmitRawEscapes(t *testing.T) {
	got := string(ansiToHTML("\x1b[31m\x1b[1mtext\x1b[0m\x1b[K"))
	if strings.ContainsRune(got, 0x1b) {
		t.Fatalf("output still contains escape byte: %q", got)
	}
}

func TestExecutionTemplateRendersAnsiLogs(t *testing.T) {
	started := time.Now().Add(-90 * time.Second)
	finished := time.Now()
	var buf strings.Builder
	data := executionPage{
		Title:     "Execution",
		Execution: stores.Execution{ID: 1, Status: stores.StatusFailed, Workflow: "build", Ref: "main", CreatedAt: time.Now()},
		Project:   stores.Project{ID: uuid.New(), Name: "demo"},
		Steps: []stepView{{
			Name:       "test",
			Status:     stores.StepStatusFailed,
			StartedAt:  &started,
			FinishedAt: &finished,
			ExitCode:   1,
			Logs:       "\x1b[31mfail\x1b[0m",
		}},
		SetupLog: "setup ok",
	}
	if err := templates.ExecuteTemplate(&buf, "execution_show", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `<span class="term-fg31">fail</span>`) {
		t.Fatalf("expected colored log span, got:\n%s", out)
	}
	if !strings.Contains(out, "exit 1") {
		t.Fatalf("expected exit code in the step header, got:\n%s", out)
	}
	if !strings.Contains(out, "1m30s") {
		t.Fatalf("expected duration in the step header, got:\n%s", out)
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Fatal("rendered page contains raw escape byte")
	}
}
