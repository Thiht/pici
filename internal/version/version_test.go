package version

import "testing"

func TestGetUsesBuildVersion(t *testing.T) {
	if got := Get("v1.2.3").Version; got != "v1.2.3" {
		t.Fatalf("expected v1.2.3, got %q", got)
	}
}
