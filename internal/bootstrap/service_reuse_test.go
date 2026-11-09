package bootstrap

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/izzzzzi/agent-assh/internal/session"
)

// liveEntry returns a registry entry that matches validRequest's target, so the
// only reason it would be rejected is the behaviour under test.
func liveEntry(req Request, sid string, createdAt time.Time) session.RegistryEntry {
	return session.RegistryEntry{
		SID:           sid,
		Label:         "deploy",
		Host:          req.Host,
		User:          req.User,
		Port:          req.Port,
		Identity:      req.Identity,
		Jump:          req.Jump,
		HostKeyPolicy: req.HostKeyPolicy,
		TmuxName:      "assh_" + sid,
		CreatedAt:     createdAt,
		TTLSeconds:    int64(req.TTL.Seconds()),
	}
}

// aliveCommand distinguishes the liveness probe from GC, whose cleanup snippet
// also calls `tmux has-session` before killing the session.
func aliveCommand(command string) bool {
	return strings.Contains(command, "assh_session_alive")
}

func TestRunReuseReturnsLiveSessionWithoutRebootstrapping(t *testing.T) {
	req := validRequest(t)
	req.Reuse = true
	entry := liveEntry(req, "aabbccdd", time.Now().UTC())
	if err := session.SaveRegistry(req.StateDir, entry); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	probed := false
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID:         func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case aliveCommand(command):
				probed = true
				if !strings.Contains(command, "assh_aabbccdd") {
					t.Fatalf("liveness probe targeted the wrong session: %s", command)
				}
				return SSHResult{ExitCode: 0, Stdout: []byte("assh_session_alive\n")}
			case command == probeCommand:
				t.Fatalf("reused session should skip the capability probe: %s", command)
				return SSHResult{}
			case strings.Contains(command, "tmux new-session"):
				t.Fatalf("reused session should not open a second tmux session: %s", command)
				return SSHResult{}
			default:
				t.Fatalf("unexpected command: %s", command)
				return SSHResult{}
			}
		},
	}

	result, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !probed {
		t.Fatal("liveness probe was never issued")
	}
	if !result.Reused {
		t.Fatal("Reused=false want true")
	}
	if result.SID != "aabbccdd" {
		t.Fatalf("SID=%q want aabbccdd", result.SID)
	}
	if result.TmuxName != "assh_aabbccdd" {
		t.Fatalf("TmuxName=%q want assh_aabbccdd", result.TmuxName)
	}
	if !result.OK || !result.KeyVerified {
		t.Fatalf("OK=%v KeyVerified=%v want true/true", result.OK, result.KeyVerified)
	}
	if result.NextCommands["exec"] == "" || result.NextCommands["list"] == "" {
		t.Fatalf("NextCommands incomplete: %v", result.NextCommands)
	}
}

func TestRunReuseFallsBackWhenSessionIsGone(t *testing.T) {
	req := validRequest(t)
	req.Reuse = true
	entry := liveEntry(req, "aabbccdd", time.Now().UTC())
	if err := session.SaveRegistry(req.StateDir, entry); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	opened := false
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID:         func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case aliveCommand(command):
				// The registered tmux session already exited.
				return SSHResult{ExitCode: 3, Stderr: []byte("session_not_found\n")}
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
			case strings.Contains(command, "tmux new-session") && strings.Contains(command, "assh_abc12345"):
				opened = true
				return SSHResult{ExitCode: 0}
			default:
				t.Fatalf("unexpected command: %s", command)
				return SSHResult{}
			}
		},
	}

	result, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Reused {
		t.Fatal("Reused=true for a dead session")
	}
	if !opened {
		t.Fatal("fallback did not open a new session")
	}
	if result.SID != "abc12345" {
		t.Fatalf("SID=%q want abc12345", result.SID)
	}
}

func TestRunReuseSkipsExpiredEntryWithoutProbing(t *testing.T) {
	req := validRequest(t)
	req.Reuse = true
	// Older than the 12h TTL, so it must not even be probed.
	expired := liveEntry(req, "aabbccdd", time.Now().UTC().Add(-13*time.Hour))
	if err := session.SaveRegistry(req.StateDir, expired); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	opened := false
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID:         func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case aliveCommand(command):
				t.Fatalf("expired entry was probed: %s", command)
				return SSHResult{}
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
			case strings.Contains(command, "tmux new-session") && strings.Contains(command, "assh_abc12345"):
				opened = true
				return SSHResult{ExitCode: 0}
			default:
				t.Fatalf("unexpected command: %s", command)
				return SSHResult{}
			}
		},
	}

	result, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Reused || !opened {
		t.Fatalf("Reused=%v opened=%v want false/true", result.Reused, opened)
	}
}

func TestRunReuseIgnoresOtherTargets(t *testing.T) {
	req := validRequest(t)
	req.Reuse = true
	other := liveEntry(req, "aabbccdd", time.Now().UTC())
	other.Host = "10.0.0.9"
	if err := session.SaveRegistry(req.StateDir, other); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	opened := false
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID:         func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case aliveCommand(command):
				t.Fatalf("entry for a different host was probed: %s", command)
				return SSHResult{}
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
			case strings.Contains(command, "tmux new-session") && strings.Contains(command, "assh_abc12345"):
				opened = true
				return SSHResult{ExitCode: 0}
			default:
				t.Fatalf("unexpected command: %s", command)
				return SSHResult{}
			}
		},
	}

	result, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Reused || !opened {
		t.Fatalf("Reused=%v opened=%v want false/true", result.Reused, opened)
	}
}

// TestRunReusePrefersNewestLiveCandidate pins the newest-first ordering. Both
// entries match the target; the newer one is alive. Probing it first means the
// older entry is never contacted, so exactly one probe is expected. Without the
// sort, os.ReadDir's filename order would probe the older one first.
func TestRunReusePrefersNewestLiveCandidate(t *testing.T) {
	req := validRequest(t)
	req.Reuse = true
	older := liveEntry(req, "aaaaaaaa", time.Now().UTC().Add(-time.Hour))
	newer := liveEntry(req, "bbbbbbbb", time.Now().UTC())
	for _, entry := range []session.RegistryEntry{older, newer} {
		if err := session.SaveRegistry(req.StateDir, entry); err != nil {
			t.Fatalf("SaveRegistry(%s) error = %v", entry.SID, err)
		}
	}

	var probed []string
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID:         func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case aliveCommand(command):
				switch {
				case strings.Contains(command, "assh_aaaaaaaa"):
					probed = append(probed, "aaaaaaaa")
					return SSHResult{ExitCode: 3, Stderr: []byte("session_not_found\n")}
				case strings.Contains(command, "assh_bbbbbbbb"):
					probed = append(probed, "bbbbbbbb")
					return SSHResult{ExitCode: 0, Stdout: []byte("assh_session_alive\n")}
				}
				t.Fatalf("probe for unknown session: %s", command)
				return SSHResult{}
			default:
				t.Fatalf("unexpected command: %s", command)
				return SSHResult{}
			}
		},
	}

	result, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.SID != "bbbbbbbb" {
		t.Fatalf("SID=%q want bbbbbbbb (newest live candidate)", result.SID)
	}
	if len(probed) != 1 || probed[0] != "bbbbbbbb" {
		t.Fatalf("probes=%v want exactly [bbbbbbbb]", probed)
	}
}

func TestRunWithoutReuseOpensNewSessionDespiteLiveEntry(t *testing.T) {
	req := validRequest(t)
	entry := liveEntry(req, "aabbccdd", time.Now().UTC())
	if err := session.SaveRegistry(req.StateDir, entry); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	opened := false
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID:         func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case aliveCommand(command):
				t.Fatalf("--reuse was not set but a liveness probe ran: %s", command)
				return SSHResult{}
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
			case strings.Contains(command, "tmux new-session") && strings.Contains(command, "assh_abc12345"):
				opened = true
				return SSHResult{ExitCode: 0}
			default:
				t.Fatalf("unexpected command: %s", command)
				return SSHResult{}
			}
		},
	}

	result, err := service.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Reused {
		t.Fatal("Reused=true without --reuse")
	}
	if !opened || result.SID != "abc12345" {
		t.Fatalf("opened=%v SID=%q want true/abc12345", opened, result.SID)
	}
}

func TestReusableTargetIdentityRules(t *testing.T) {
	base := validRequest(t)
	tests := []struct {
		name  string
		entry session.RegistryEntry
		req   Request
		want  bool
	}{
		{
			name:  "identical target",
			entry: liveEntry(base, "aabbccdd", time.Now().UTC()),
			req:   base,
			want:  true,
		},
		{
			name: "different host",
			entry: func() session.RegistryEntry {
				e := liveEntry(base, "aabbccdd", time.Now().UTC())
				e.Host = "10.0.0.9"
				return e
			}(),
			req:  base,
			want: false,
		},
		{
			name: "different user",
			entry: func() session.RegistryEntry {
				e := liveEntry(base, "aabbccdd", time.Now().UTC())
				e.User = "ubuntu"
				return e
			}(),
			req:  base,
			want: false,
		},
		{
			name: "different port",
			entry: func() session.RegistryEntry {
				e := liveEntry(base, "aabbccdd", time.Now().UTC())
				e.Port = 2222
				return e
			}(),
			req:  base,
			want: false,
		},
		{
			name: "both identities set and different",
			entry: func() session.RegistryEntry {
				e := liveEntry(base, "aabbccdd", time.Now().UTC())
				e.Identity = "/keys/other"
				return e
			}(),
			req:  base,
			want: false,
		},
		{
			name: "entry identity empty means ssh default",
			entry: func() session.RegistryEntry {
				e := liveEntry(base, "aabbccdd", time.Now().UTC())
				e.Identity = ""
				return e
			}(),
			req:  base,
			want: true,
		},
		{
			name:  "request identity empty means ssh default",
			entry: liveEntry(base, "aabbccdd", time.Now().UTC()),
			req:   func() Request { r := base; r.Identity = ""; return r }(),
			want:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := reusableTarget(test.entry, test.req); got != test.want {
				t.Fatalf("reusableTarget()=%v want %v", got, test.want)
			}
		})
	}
}

// TestNextCommandsExposeSessionLifecycle pins the agent hot path: the four
// lifecycle commands, sid-filled where a sid applies, and no operator-facing
// commands (gc/audit) leaking in.
func TestNextCommandsExposeSessionLifecycle(t *testing.T) {
	got := nextCommands("f7a2b3c4")
	want := map[string]string{
		"exec":  `assh session exec -s f7a2b3c4 -- pwd`,
		"read":  "assh session read -s f7a2b3c4 --seq 1 --limit 50",
		"list":  "assh session list",
		"close": "assh session close -s f7a2b3c4",
	}
	if len(got) != len(want) {
		t.Fatalf("nextCommands() has %d keys (%v) want %d", len(got), got, len(want))
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("nextCommands()[%q]=%q want %q", key, got[key], value)
		}
	}
	for _, operatorFacing := range []string{"gc", "audit"} {
		if _, ok := got[operatorFacing]; ok {
			t.Fatalf("operator-facing key %q leaked into the agent hot path", operatorFacing)
		}
	}
}

func TestReusableTargetRequiresSameProfile(t *testing.T) {
	entry := session.RegistryEntry{Host: "h", User: "u", Port: 22, Profile: "readonly"}
	if reusableTarget(entry, Request{Host: "h", User: "u", Port: 22}) {
		t.Fatal("a readonly session must not be reused for an unrestricted connect")
	}
	if !reusableTarget(entry, Request{Host: "h", User: "u", Port: 22, Profile: "readonly"}) {
		t.Fatal("same profile should be reusable")
	}
}
