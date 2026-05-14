package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/agent-ssh/assh/internal/capabilities"
	"github.com/agent-ssh/assh/internal/session"
)

const keyCheckCommand = "true"

var probeCommand = capabilities.ProbeCommand()

const installTmuxRemoteCommand = "if command -v apt >/dev/null 2>&1; then sudo -n apt update >/dev/null 2>&1 && sudo -n apt install -y tmux || { echo tmux_install_failed >&2; exit 1; }; " +
	"elif command -v dnf >/dev/null 2>&1; then sudo -n dnf install -y tmux || { echo tmux_install_failed >&2; exit 1; }; " +
	"elif command -v yum >/dev/null 2>&1; then sudo -n yum install -y tmux || { echo tmux_install_failed >&2; exit 1; }; " +
	"elif command -v apk >/dev/null 2>&1; then sudo -n apk add tmux || { echo tmux_install_failed >&2; exit 1; }; " +
	"elif command -v pacman >/dev/null 2>&1; then sudo -n pacman -Sy --noconfirm tmux || { echo tmux_install_failed >&2; exit 1; }; " +
	"elif command -v brew >/dev/null 2>&1; then brew install tmux || { echo tmux_install_failed >&2; exit 1; }; " +
	"else echo tmux_missing >&2; exit 127; fi"

type Request struct {
	Host            string
	User            string
	Port            int
	Identity        string
	PasswordEnv     string
	SessionName     string
	TTL             time.Duration
	Timeout         time.Duration
	HostKeyPolicy   string
	GCOlderThan     time.Duration
	SkipGC          bool
	SkipTmuxInstall bool
	StateDir        string
}

type Result struct {
	OK            bool              `json:"ok"`
	Host          string            `json:"host"`
	User          string            `json:"user"`
	Identity      string            `json:"identity"`
	KeyDeployed   bool              `json:"key_deployed"`
	KeyVerified   bool              `json:"key_verified"`
	TmuxInstalled bool              `json:"tmux_installed"`
	GCDeleted     []string          `json:"gc_deleted"`
	GCErrors      []GCError         `json:"gc_errors,omitempty"`
	SID           string            `json:"sid"`
	Session       string            `json:"session"`
	TmuxName      string            `json:"tmux_name"`
	NextCommands  map[string]string `json:"next_commands"`
}

type GCError struct {
	SID   string `json:"sid"`
	Error string `json:"error"`
}

type Error struct {
	Code    string
	Message string
	Hint    string
}

func (e Error) Error() string { return e.Message }

type SSHRunner func(context.Context, SSHTarget, string) SSHResult
type KeyEnsurer func(string) error
type PasswordDeployer func(context.Context, string, SSHTarget, string) error
type EnvLookup func(string) (string, bool)
type IDGenerator func() (string, error)

type SSHTarget struct {
	Host          string
	User          string
	Port          int
	Identity      string
	TimeoutSecond int
	HostKeyPolicy string
}

type SSHResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error
}

type Service struct {
	RunSSH         SSHRunner
	EnsureKeyPair  KeyEnsurer
	DeployPassword PasswordDeployer
	LookupEnv      EnvLookup
	NewID          IDGenerator
}

func (s Service) Run(ctx context.Context, req Request) (Result, error) {
	if err := validate(req); err != nil {
		return Result{}, err
	}
	if s.EnsureKeyPair == nil || s.RunSSH == nil || s.NewID == nil {
		return Result{}, Error{Code: "internal_error", Message: "bootstrap dependencies are not configured"}
	}
	if err := s.EnsureKeyPair(req.Identity); err != nil {
		return Result{}, Error{Code: "internal_error", Message: err.Error()}
	}

	target := SSHTarget{
		Host:          req.Host,
		User:          req.User,
		Port:          req.Port,
		Identity:      req.Identity,
		TimeoutSecond: int(req.Timeout.Seconds()),
		HostKeyPolicy: req.HostKeyPolicy,
	}

	keyDeployed := false
	keyResult := s.RunSSH(ctx, target, keyCheckCommand)
	if code := sshErrorCode(ctx.Err(), keyResult); code != "" {
		if code != "auth_failed" {
			return Result{}, Error{Code: code, Message: sshErrorMessage(ctx.Err(), keyResult)}
		}
		if req.PasswordEnv == "" {
			return Result{}, Error{
				Code:    "auth_failed",
				Message: "key login failed and no password env was provided",
				Hint:    "retry with -E PASSWORD_ENV or configure SSH keys",
			}
		}
		if s.LookupEnv == nil {
			return Result{}, Error{Code: "internal_error", Message: "environment lookup is not configured"}
		}
		password, ok := s.LookupEnv(req.PasswordEnv)
		if !ok || password == "" {
			return Result{}, Error{
				Code:    "auth_failed",
				Message: "password env is empty",
				Hint:    "set " + req.PasswordEnv + " before running connect",
			}
		}
		if s.DeployPassword == nil {
			return Result{}, Error{Code: "internal_error", Message: "password deployer is not configured"}
		}
		if err := s.DeployPassword(ctx, password, target, req.Identity); err != nil {
			return Result{}, Error{Code: "key_deploy_failed", Message: err.Error()}
		}
		keyDeployed = true

		verify := s.RunSSH(ctx, target, keyCheckCommand)
		if code := sshErrorCode(ctx.Err(), verify); code != "" {
			return Result{}, Error{Code: "key_deploy_failed", Message: "key deployment completed but key login verification failed"}
		}
	}

	return s.finishAfterAuth(ctx, req, target, keyDeployed)
}

func (s Service) finishAfterAuth(ctx context.Context, req Request, target SSHTarget, keyDeployed bool) (Result, error) {
	probeResult := s.RunSSH(ctx, target, probeCommand)
	if code := sshErrorCode(ctx.Err(), probeResult); code != "" {
		return Result{}, Error{Code: code, Message: sshErrorMessage(ctx.Err(), probeResult)}
	}
	probe := capabilities.ParseProbe(probeResult.Stdout)
	if probe.SessionBackend != "tmux" {
		return Result{}, Error{Code: "tmux_missing", Message: "unsupported remote session backend"}
	}

	tmuxInstalled := probe.TmuxInstalled
	if !probe.TmuxInstalled {
		if req.SkipTmuxInstall {
			return Result{}, Error{Code: "tmux_missing", Message: "tmux is not installed"}
		}
		installResult := s.RunSSH(ctx, target, installTmuxRemoteCommand)
		if code := sshErrorCode(ctx.Err(), installResult); code != "" {
			return Result{}, Error{Code: installErrorCode(code), Message: sshErrorMessage(ctx.Err(), installResult)}
		}
		tmuxInstalled = true
	}

	gcDeleted, gcErrors, err := s.runGC(ctx, req, target)
	if err != nil {
		return Result{}, err
	}

	sid, err := s.NewID()
	if err != nil {
		return Result{}, Error{Code: "internal_error", Message: err.Error()}
	}
	metadata := session.NewMetadata(sid, req.SessionName, req.TTL, "")
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return Result{}, Error{Code: "internal_error", Message: err.Error()}
	}
	openCommand, err := session.OpenRemoteCommand(string(metaJSON), metadata.TmuxName)
	if err != nil {
		return Result{}, Error{Code: "internal_error", Message: err.Error()}
	}
	openResult := s.RunSSH(ctx, target, openCommand)
	if code := sshErrorCode(ctx.Err(), openResult); code != "" {
		return Result{}, Error{Code: code, Message: sshErrorMessage(ctx.Err(), openResult)}
	}

	entry := session.RegistryEntry{
		SID:           metadata.SID,
		Label:         metadata.Label,
		Host:          req.Host,
		User:          req.User,
		Port:          req.Port,
		Identity:      req.Identity,
		HostKeyPolicy: req.HostKeyPolicy,
		TmuxName:      metadata.TmuxName,
		CreatedAt:     metadata.CreatedAt,
		TTLSeconds:    metadata.TTLSeconds,
		Seq:           0,
	}
	if err := session.SaveRegistry(req.StateDir, entry); err != nil {
		return Result{}, Error{Code: "internal_error", Message: err.Error()}
	}

	return Result{
		OK:            true,
		Host:          req.Host,
		User:          req.User,
		Identity:      req.Identity,
		KeyDeployed:   keyDeployed,
		KeyVerified:   true,
		TmuxInstalled: tmuxInstalled,
		GCDeleted:     gcDeleted,
		GCErrors:      gcErrors,
		SID:           sid,
		Session:       req.SessionName,
		TmuxName:      metadata.TmuxName,
		NextCommands: map[string]string{
			"exec":  `assh session exec -s ` + sid + ` -- "pwd"`,
			"read":  "assh session read -s " + sid + " --seq 1 --limit 50",
			"close": "assh session close -s " + sid,
		},
	}, nil
}

func (s Service) runGC(ctx context.Context, req Request, target SSHTarget) ([]string, []GCError, error) {
	if req.SkipGC {
		return nil, nil, nil
	}
	entries, err := session.ListRegistry(req.StateDir)
	if err != nil {
		return nil, nil, Error{Code: "internal_error", Message: err.Error()}
	}

	now := time.Now().UTC()
	var deleted []string
	var gcErrors []GCError
	for _, entry := range entries {
		if entry.Host != req.Host || entry.User != req.User || entry.Port != req.Port {
			continue
		}
		if req.GCOlderThan > 0 && !entry.CreatedAt.Add(req.GCOlderThan).Before(now) {
			continue
		}

		command, err := session.GCRemoteCommand(entry.SID, entry.TmuxName)
		if err != nil {
			gcErrors = append(gcErrors, GCError{SID: entry.SID, Error: err.Error()})
			continue
		}
		gcTarget := target
		gcTarget.Identity = entry.Identity
		gcTarget.HostKeyPolicy = entry.HostKeyPolicy
		result := s.RunSSH(ctx, gcTarget, command)
		if code := sshErrorCode(ctx.Err(), result); code != "" {
			gcErrors = append(gcErrors, GCError{SID: entry.SID, Error: sshErrorMessage(ctx.Err(), result)})
			continue
		}
		if err := session.DeleteRegistry(req.StateDir, entry.SID); err != nil {
			gcErrors = append(gcErrors, GCError{SID: entry.SID, Error: err.Error()})
			continue
		}
		deleted = append(deleted, entry.SID)
	}
	return deleted, gcErrors, nil
}

func installErrorCode(code string) string {
	switch code {
	case "tmux_missing", "command_failed", "connection_error":
		return "tmux_install_failed"
	default:
		return code
	}
}

func sshErrorCode(ctxErr error, result SSHResult) string {
	if ctxErr != nil {
		return "timeout"
	}
	if result.Err == nil && result.ExitCode == 0 {
		return ""
	}
	var execErr *exec.Error
	if errors.As(result.Err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return "ssh_missing"
	}

	text := strings.ToLower(strings.Join([]string{
		string(result.Stderr),
		string(result.Stdout),
		errorText(result.Err),
	}, "\n"))
	switch {
	case strings.Contains(text, "permission denied"), strings.Contains(text, "authentication failed"):
		return "auth_failed"
	case strings.Contains(text, "host key verification failed"), strings.Contains(text, "remote host identification has changed"):
		return "host_key_failed"
	case strings.Contains(text, "tmux_missing"):
		return "tmux_missing"
	case strings.Contains(text, "tmux_install_failed"):
		return "tmux_install_failed"
	case result.ExitCode == 127:
		return "ssh_missing"
	case result.ExitCode != 0:
		return "connection_error"
	default:
		return "connection_error"
	}
}

func sshErrorMessage(ctxErr error, result SSHResult) string {
	if ctxErr != nil {
		return ctxErr.Error()
	}
	text := strings.TrimSpace(strings.Join([]string{
		string(result.Stderr),
		string(result.Stdout),
		errorText(result.Err),
	}, "\n"))
	if text != "" {
		return text
	}
	if result.ExitCode != 0 {
		return "ssh command failed"
	}
	return "ssh command failed"
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func validate(req Request) error {
	if req.Host == "" {
		return Error{Code: "invalid_args", Message: "host required"}
	}
	if req.Port < 1 || req.Port > 65535 {
		return Error{Code: "invalid_args", Message: "port must be between 1 and 65535"}
	}
	if req.Timeout <= 0 {
		return Error{Code: "invalid_args", Message: "timeout must be greater than 0"}
	}
	if req.TTL <= 0 {
		return Error{Code: "invalid_args", Message: "ttl must be greater than 0"}
	}
	switch req.HostKeyPolicy {
	case "accept-new", "strict", "no-check":
		return nil
	default:
		return Error{Code: "invalid_args", Message: "invalid host key policy"}
	}
}
