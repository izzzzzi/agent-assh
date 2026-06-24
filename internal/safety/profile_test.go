package safety

import "testing"

func TestMatchProfileReadonly(t *testing.T) {
	p := &Profiles{
		Profile: map[string]Profile{
			"readonly": {Allow: []string{"journalctl *", "tail *", "df -h", "cat *", "systemctl status *"}},
		},
	}
	tests := []struct {
		cmd  string
		want bool // true = dangerous (blocked)
	}{
		{"journalctl -u nginx", false},
		{"df -h", false},
		{"tail -f /var/log/syslog", false},
		{"apt install nginx", true},
		{"cat /etc/passwd", false},
		{"systemctl status docker", false},
	}
	for _, tc := range tests {
		r := p.Match("readonly", tc.cmd)
		if r.Dangerous != tc.want {
			t.Errorf("Match(readonly, %q): got dangerous=%v, want %v (msg: %s)", tc.cmd, r.Dangerous, tc.want, r.Message)
		}
	}
}

func TestMatchAdmin(t *testing.T) {
	p := &Profiles{
		Profile: map[string]Profile{"admin": {Allow: []string{"*"}}},
	}
	for _, cmd := range []string{"rm -rf /", "apt install nginx", "anything at all", "reboot"} {
		r := p.Match("admin", cmd)
		if r.Dangerous {
			t.Errorf("Match(admin, %q): got dangerous=true, want false", cmd)
		}
	}
}

func TestMatchMissingProfile(t *testing.T) {
	p := &Profiles{}
	r := p.Match("nonexistent", "anything")
	if !r.Dangerous {
		t.Error("expected dangerous for missing profile")
	}
	if r.Rule != "profile:not_found" {
		t.Errorf("expected rule 'profile:not_found', got %q", r.Rule)
	}
}

func TestMatchEmptyCommand(t *testing.T) {
	p := &Profiles{
		Profile: map[string]Profile{"test": {Allow: []string{"*"}}},
	}
	r := p.Match("test", "")
	if !r.Dangerous {
		t.Error("expected dangerous for empty command")
	}
	r = p.Match("test", "   ")
	if !r.Dangerous {
		t.Error("expected dangerous for whitespace-only command")
	}
}

func TestMatchWildcardArgs(t *testing.T) {
	p := &Profiles{
		Profile: map[string]Profile{
			"test": {Allow: []string{"docker logs *", "cat /var/log/*"}},
		},
	}
	// docker logs with any args
	if r := p.Match("test", "docker logs myapp"); r.Dangerous {
		t.Errorf("expected allowed, got: %s", r.Message)
	}
	if r := p.Match("test", "docker logs -f --tail 100 web"); r.Dangerous {
		t.Errorf("expected allowed, got: %s", r.Message)
	}
	// cat with /var/log/ prefix
	if r := p.Match("test", "cat /var/log/syslog"); r.Dangerous {
		t.Errorf("expected allowed, got: %s", r.Message)
	}
	// not allowed
	if r := p.Match("test", "cat /etc/passwd"); !r.Dangerous {
		t.Error("expected blocked for /etc/ file")
	}
	if r := p.Match("test", "docker restart myapp"); !r.Dangerous {
		t.Error("expected blocked for docker restart")
	}
}
