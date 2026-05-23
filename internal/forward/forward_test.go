package forward

import (
	"path/filepath"
	"testing"
	"time"
)

func TestControlSocketPathIsStableAndStateLocal(t *testing.T) {
	got := ControlSocketPath("/tmp/assh-state", "deploy")
	want := filepath.Join("/tmp/assh-state", "forward", "sockets", "deploy.sock")
	if got != want {
		t.Fatalf("ControlSocketPath() = %q, want %q", got, want)
	}
}

func TestStartArgsIncludeControlSocketAndRules(t *testing.T) {
	spec := Spec{
		Host:          "example.com",
		User:          "root",
		Port:          2222,
		Identity:      "key",
		Jump:          "bastion.example.com",
		TimeoutSecond: 30,
		HostKeyPolicy: "strict",
		ControlSocket: "/tmp/assh-forward.sock",
		Persist:       2 * time.Hour,
		Local:         []string{"127.0.0.1:8080:127.0.0.1:80"},
		Remote:        []string{"9000:127.0.0.1:9000"},
		Dynamic:       []string{"127.0.0.1:1080"},
	}

	args := StartArgs(spec)

	for _, want := range [][]string{
		{"-N"},
		{"-f"},
		{"-M"},
		{"-S", "/tmp/assh-forward.sock"},
		{"-o", "ControlPersist=2h0m0s"},
		{"-L", "127.0.0.1:8080:127.0.0.1:80"},
		{"-R", "9000:127.0.0.1:9000"},
		{"-D", "127.0.0.1:1080"},
		{"-J", "bastion.example.com"},
		{"--", "root@example.com"},
	} {
		if !containsSubsequence(args, want...) {
			t.Fatalf("StartArgs() = %#v, missing %v", args, want)
		}
	}
}

func TestControlArgsUseCheckAndExit(t *testing.T) {
	spec := Spec{Host: "example.com", User: "root", ControlSocket: "/tmp/assh-forward.sock"}

	check := ControlArgs(spec, "check")
	exit := ControlArgs(spec, "exit")

	if !containsSubsequence(check, "-S", "/tmp/assh-forward.sock", "-O", "check") {
		t.Fatalf("check args = %#v", check)
	}
	if !containsSubsequence(exit, "-S", "/tmp/assh-forward.sock", "-O", "exit") {
		t.Fatalf("exit args = %#v", exit)
	}
}

func containsSubsequence(values []string, subsequence ...string) bool {
	if len(subsequence) == 0 {
		return true
	}
	for i := 0; i <= len(values)-len(subsequence); i++ {
		ok := true
		for j, want := range subsequence {
			if values[i+j] != want {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
