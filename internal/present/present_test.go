package present

import (
	"slices"
	"testing"
	"time"

	"github.com/Thiht/pici/internal/stores"
)

func TestDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                         "0s",
		15 * time.Second:          "15s",
		15 * time.Minute:          "15m",
		90 * time.Minute:          "1h30m",
		time.Hour:                 "1h",
		time.Hour + 5*time.Minute: "1h5m",
	}
	for d, want := range cases {
		if got := Duration(d); got != want {
			t.Errorf("Duration(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestSize(t *testing.T) {
	cases := map[int64]string{
		0:       "0 B",
		1023:    "1023 B",
		2048:    "2.0 KB",
		1 << 20: "1.0 MB",
		3 << 30: "3.0 GB",
	}
	for n, want := range cases {
		if got := Size(n); got != want {
			t.Errorf("Size(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestLabel(t *testing.T) {
	cases := map[string]string{"success": "Success", "running": "Running", "": "", "x": "X"}
	for status, want := range cases {
		if got := Label(status); got != want {
			t.Errorf("Label(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestSHA(t *testing.T) {
	long := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c"
	if got := SHA(long); got != "2cf24db" {
		t.Fatalf("SHA() = %q", got)
	}
	if got := SHA("abc"); got != "abc" {
		t.Fatalf("SHA(short) = %q", got)
	}
}

func TestStatusMappingsCoverEveryStatus(t *testing.T) {
	names := slices.Concat(stores.StatusNames(), stores.StepStatusNames())
	for _, name := range names {
		if Label(name) == "" {
			t.Errorf("no label for %q", name)
		}
		if Class(name) == "" {
			t.Errorf("no class for %q", name)
		}
		if _, ok := Classes()[name]; !ok && Class(name) != "badge badge-soft badge-neutral" {
			t.Errorf("%q is missing from the class map", name)
		}
		if DotClass(name) == "" {
			t.Errorf("no dot class for %q", name)
		}
		if Symbol(name) == "?" {
			t.Errorf("no symbol for %q", name)
		}
	}
}

func TestClassTable(t *testing.T) {
	cases := map[string]string{
		"success":  "badge badge-soft badge-success",
		"failed":   "badge badge-soft badge-error",
		"running":  "badge badge-soft badge-info",
		"pending":  "badge badge-soft badge-warning",
		"canceled": "badge badge-soft badge-neutral",
		"skipped":  "badge badge-soft badge-neutral",
	}
	for status, want := range cases {
		if got := Class(status); got != want {
			t.Errorf("Class(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestDotClassTable(t *testing.T) {
	cases := map[string]string{
		"success":  "bg-success text-success-content",
		"failed":   "bg-error text-error-content",
		"running":  "bg-info text-info-content",
		"pending":  "bg-warning text-warning-content",
		"canceled": "bg-neutral text-neutral-content",
		"skipped":  "bg-neutral text-neutral-content",
	}
	for status, want := range cases {
		if got := DotClass(status); got != want {
			t.Errorf("DotClass(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestUnknownStatus(t *testing.T) {
	if got := Class("nonsense"); got != "badge badge-soft badge-neutral" {
		t.Errorf("Class(unknown) = %q", got)
	}
	if got := DotClass("nonsense"); got != "bg-neutral text-neutral-content" {
		t.Errorf("DotClass(unknown) = %q", got)
	}
	if value, color := Badge("nonsense"); value != "unknown" || color == "" {
		t.Errorf("Badge(unknown) = %q, %q", value, color)
	}
	if got := Symbol("nonsense"); got != "?" {
		t.Errorf("Symbol(unknown) = %q", got)
	}
}
