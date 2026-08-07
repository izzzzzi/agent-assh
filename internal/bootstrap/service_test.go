package bootstrap

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/izzzzzi/agent-assh/internal/session"
)

func TestRunOpensSessionWhenTmuxAlreadyInstalled(t *testing.T) {
	req := validRequest(t)
	installed := false
	opened := false
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID:         func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
			case command == installTmuxRemoteCommand:
				installed = true
				return SSHResult{ExitCode: 0}
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
	if installed {
		t.Fatal("install command was called")
	}
	if !opened {
		t.Fatal("open command was not called")
	}
	if !result.TmuxInstalled {
		t.Fatal("TmuxInstalled=false want true")
	}
}

func TestRunGCOldMatchingEntryBeforeOpeningNewSession(t *testing.T) {
	req := validRequest(t)
	now := time.Now().UTC()
	oldEntry := session.RegistryEntry{
		SID:           "deadbeef",
		Label:         "old",
		Host:          req.Host,
		User:          req.User,
		Port:          req.Port,
		Identity:      req.Identity,
		Jump:          "stored-bastion.example.com",
		HostKeyPolicy: req.HostKeyPolicy,
		TmuxName:      "assh_deadbeef",
		CreatedAt:     now.Add(-2 * req.GCOlderThan),
		TTLSeconds:    int64(req.TTL.Seconds()),
	}
	req.Jump = ""
	recentEntry := oldEntry
	recentEntry.SID = "cafebabe"
	recentEntry.TmuxName = "assh_cafebabe"
	recentEntry.CreatedAt = now
	if err := session.SaveRegistry(req.StateDir, oldEntry); err != nil {
		t.Fatalf("SaveRegistry(old) error = %v", err)
	}
	if err := session.SaveRegistry(req.StateDir, recentEntry); err != nil {
		t.Fatalf("SaveRegistry(recent) error = %v", err)
	}

	gcCalled := false
	opened := false
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID:         func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, target SSHTarget, command string) SSHResult {
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
			case strings.Contains(command, "deadbeef") &&
				strings.Contains(command, "metadata_validation_failed") &&
				strings.Contains(command, "tmux kill-session"):
				gcCalled = true
				if target.Identity != oldEntry.Identity ||
					target.HostKeyPolicy != oldEntry.HostKeyPolicy ||
					target.Jump != oldEntry.Jump {
					t.Fatalf("gc target identity=%q policy=%q jump=%q", target.Identity, target.HostKeyPolicy, target.Jump)
				}
				return SSHResult{ExitCode: 0}
			case strings.Contains(command, "cafebabe"):
				t.Fatalf("recent registry entry was garbage-collected: %s", command)
				return SSHResult{}
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
	if !gcCalled {
		t.Fatal("remote GC was not called for old matching entry")
	}
	if !opened {
		t.Fatal("new session was not opened")
	}
	if !slices.Equal(result.GCDeleted, []string{"deadbeef"}) {
		t.Fatalf("GCDeleted=%v want [deadbeef]", result.GCDeleted)
	}
	if _, err := session.LoadRegistry(req.StateDir, "deadbeef"); err == nil {
		t.Fatal("old registry entry still exists")
	}
	if _, err := session.LoadRegistry(req.StateDir, "cafebabe"); err != nil {
		t.Fatalf("recent registry entry missing: %v", err)
	}
}

func TestRunSkipGCDoesNotCallRemoteGC(t *testing.T) {
	req := validRequest(t)
	req.SkipGC = true
	oldEntry := session.RegistryEntry{
		SID:           "deadbeef",
		Label:         "old",
		Host:          req.Host,
		User:          req.User,
		Port:          req.Port,
		Identity:      req.Identity,
		HostKeyPolicy: req.HostKeyPolicy,
		TmuxName:      "assh_deadbeef",
		CreatedAt:     time.Now().UTC().Add(-2 * req.GCOlderThan),
		TTLSeconds:    int64(req.TTL.Seconds()),
	}
	if err := session.SaveRegistry(req.StateDir, oldEntry); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID:         func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
			case strings.Contains(command, "deadbeef") && strings.Contains(command, "tmux kill-session"):
				t.Fatalf("remote GC was called despite SkipGC: %s", command)
				return SSHResult{}
			case strings.Contains(command, "tmux new-session") && strings.Contains(command, "assh_abc12345"):
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
	if len(result.GCDeleted) != 0 || len(result.GCErrors) != 0 {
		t.Fatalf("gc result = deleted:%v errors:%v", result.GCDeleted, result.GCErrors)
	}
	if _, err := session.LoadRegistry(req.StateDir, "deadbeef"); err != nil {
		t.Fatalf("old registry entry missing: %v", err)
	}
}

func TestRunGCRemoteFailureRecordsErrorAndKeepsRegistry(t *testing.T) {
	req := validRequest(t)
	oldEntry := session.RegistryEntry{
		SID:           "deadbeef",
		Label:         "old",
		Host:          req.Host,
		User:          req.User,
		Port:          req.Port,
		Identity:      req.Identity,
		HostKeyPolicy: req.HostKeyPolicy,
		TmuxName:      "assh_deadbeef",
		CreatedAt:     time.Now().UTC().Add(-2 * req.GCOlderThan),
		TTLSeconds:    int64(req.TTL.Seconds()),
	}
	if err := session.SaveRegistry(req.StateDir, oldEntry); err != nil {
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
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=installed\npkg=apt\ninstall=noninteractive\n")}
			case strings.Contains(command, "deadbeef") &&
				strings.Contains(command, "metadata_validation_failed") &&
				strings.Contains(command, "tmux kill-session"):
				return SSHResult{ExitCode: 3, Stderr: []byte("metadata_validation_failed")}
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
	if !opened {
		t.Fatal("new session was not opened")
	}
	if len(result.GCDeleted) != 0 {
		t.Fatalf("GCDeleted=%v want empty", result.GCDeleted)
	}
	if len(result.GCErrors) != 1 ||
		result.GCErrors[0].SID != "deadbeef" ||
		!strings.Contains(result.GCErrors[0].Error, "metadata_validation_failed") {
		t.Fatalf("GCErrors=%v want metadata failure for deadbeef", result.GCErrors)
	}
	if _, err := session.LoadRegistry(req.StateDir, "deadbeef"); err != nil {
		t.Fatalf("old registry entry missing after remote GC failure: %v", err)
	}
}

func TestRunReturnsTmuxMissingWhenInstallDisabled(t *testing.T) {
	req := validRequest(t)
	req.SkipTmuxInstall = true
	installed := false
	opened := false
	service := Service{
		EnsureKeyPair: func(string) error { return nil },
		NewID:         func() (string, error) { return "abc12345", nil },
		RunSSH: func(_ context.Context, _ SSHTarget, command string) SSHResult {
			switch {
			case command == keyCheckCommand:
				return SSHResult{ExitCode: 0}
			case command == probeCommand:
				return SSHResult{ExitCode: 0, Stdout: []byte("os=linux\ntmux=missing\npkg=apt\ninstall=noninteractive\n")}
			case command == installTmuxRemoteCommand:
				installed = true
				return SSHResult{ExitCode: 0}
			case strings.Contains(command, "tmux new-session"):
				opened = true
				return SSHResult{ExitCode: 0}
			default:
				t.Fatalf("unexpected command: %s", command)
				return SSHResult{}
			}
		},
	}

	_, err := service.Run(context.Background(), req)
	if err == nil {
		t.Fatal("expected tmux_missing")
	}
	bootErr := err.(Error)
	if bootErr.Code != "tmux_missing" {
		t.Fatalf("code=%q want tmux_missing", bootErr.Code)
	}
	if installed {
		t.Fatal("install command was called")
	}
	if opened {
		t.Fatal("open command was called")
	}
}
