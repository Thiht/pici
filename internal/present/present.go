package present

import (
	"fmt"
	"strings"
	"time"
)

// Duration renders a duration without trailing zero units, so 15m0s reads as
// 15m and 1h0m0s as 1h.
func Duration(d time.Duration) string {
	s := d.String()
	if d >= time.Minute && strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
		if strings.HasSuffix(s, "h0m") {
			s = strings.TrimSuffix(s, "0m")
		}
	}
	return s
}

func Size(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func SHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func Label(status string) string {
	if status == "" {
		return ""
	}
	r := []rune(status)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

var classes = map[string]string{
	"success":  "badge badge-soft badge-success",
	"failed":   "badge badge-soft badge-error",
	"running":  "badge badge-soft badge-info",
	"pending":  "badge badge-soft badge-warning",
	"canceled": "badge badge-soft badge-neutral",
	"skipped":  "badge badge-soft badge-neutral",
}

var dotClasses = map[string]string{
	"success":  "bg-success text-success-content",
	"failed":   "bg-error text-error-content",
	"running":  "bg-info text-info-content",
	"pending":  "bg-warning text-warning-content",
	"canceled": "bg-neutral text-neutral-content",
	"skipped":  "bg-neutral text-neutral-content",
}

func Class(status string) string {
	if class, ok := classes[status]; ok {
		return class
	}
	return "badge badge-soft badge-neutral"
}

// DotClass is the class set of a filled status dot, with the status symbol
// drawn in the contrasting content color.
func DotClass(status string) string {
	if class, ok := dotClasses[status]; ok {
		return class
	}
	return "bg-neutral text-neutral-content"
}

// Classes is the status-to-class map, so a page can hand it to its own
// client-side code instead of keeping a second copy.
func Classes() map[string]string {
	return classes
}

func Symbol(status string) string {
	switch status {
	case "success":
		return "✓"
	case "failed":
		return "✗"
	case "running":
		return "▶"
	case "pending":
		return "○"
	case "canceled":
		return "⊘"
	case "skipped":
		return "-"
	default:
		return "?"
	}
}

func Badge(status string) (value, color string) {
	switch status {
	case "success":
		return "passing", "#2ea44f"
	case "failed":
		return "failing", "#d73a49"
	case "running":
		return "running", "#0969da"
	case "pending":
		return "pending", "#dbab09"
	case "canceled":
		return "canceled", "#6e7781"
	default:
		return "unknown", "#9f9f9f"
	}
}
