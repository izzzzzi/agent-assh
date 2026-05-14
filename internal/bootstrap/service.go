package bootstrap

import (
	"context"
	"time"
)

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
	NewID          IDGenerator
}

func (s Service) Run(ctx context.Context, req Request) (Result, error) {
	if err := validate(req); err != nil {
		return Result{}, err
	}
	return Result{}, Error{Code: "internal_error", Message: "bootstrap dependencies are not configured"}
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
