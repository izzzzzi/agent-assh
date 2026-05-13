package capabilities

import "testing"

func TestParseProbe(t *testing.T) {
	raw := "os=linux\ntmux=missing\npkg=apt\ninstall=noninteractive\n"

	got := ParseProbe([]byte(raw))

	if got.OS != "linux" {
		t.Fatalf("OS = %q, want linux", got.OS)
	}
	if got.TmuxInstalled {
		t.Fatalf("TmuxInstalled = true, want false")
	}
	if got.PackageManager != "apt" {
		t.Fatalf("PackageManager = %q, want apt", got.PackageManager)
	}
	if !got.NonInteractiveInstall {
		t.Fatalf("NonInteractiveInstall = false, want true")
	}
}
