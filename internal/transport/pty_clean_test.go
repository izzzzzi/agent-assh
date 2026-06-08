package transport

import "testing"

func TestCleanPTYOutput_cleanOutputPassesThrough(t *testing.T) {
	in := []byte("hello\nworld\n")
	got := string(CleanPTYOutput(in, ""))
	if got != "hello\nworld" {
		t.Fatalf("clean output modified: %q", got)
	}
}

func TestCleanPTYOutput_ansiEscapesStripped(t *testing.T) {
	// Use raw bytes to avoid Go string escaping issues
	esc := byte(0x1b)
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{"cursor movement", []byte(string(esc) + "[Ahello" + string(esc) + "[K"), "hello"},
		{"color codes", []byte(string(esc) + "[31mred" + string(esc) + "[0m"), "red"},
		{"DEC private mode set", []byte(string(esc) + "[?2004hhello"), "hello"},
		{"DEC private mode reset", []byte(string(esc) + "[?2004lworld"), "world"},
		{"window title BEL", []byte(string(esc) + "]0;title" + "\x07hello"), "hello"},
		{"window title ESC", []byte(string(esc) + "]0;title" + string(esc) + "\\hello"), "hello"},
		{"erase display", []byte(string(esc) + "[2Jclear"), "clear"},
		{"erase line", []byte(string(esc) + "[Kdata"), "data"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(CleanPTYOutput(tt.input, ""))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanPTYOutput_carriageReturnsNormalized(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"CRLF to LF", "line1\r\nline2\r\n", "line1\nline2"},
		{"bare CR to LF", "line1\rline2\r", "line1\nline2"},
		{"mixed", "a\r\nb\rc\n", "a\nb\nc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(CleanPTYOutput([]byte(tt.input), ""))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanPTYOutput_shellPromptsStripped(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"root prompt", "root@host:/path# \noutput", "output"},
		{"user prompt", "user@box:~/dir$ \nresult", "result"},
		{"bare dollar", "$\ncmd", "cmd"},
		{"bare hash", "# \ncmd", "cmd"},
		{"prompt with trailing output", "root@host:/# \nline1\nline2", "line1\nline2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(CleanPTYOutput([]byte(tt.input), ""))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanPTYOutput_commandEchoStripped(t *testing.T) {
	tests := []struct {
		name  string
		input string
		cmd   string
		want  string
	}{
		{"exact echo", "ls -la /tmp\nfile1\nfile2", "ls -la /tmp", "file1\nfile2"},
		{"prefixed with $", "$ ls -la /tmp\nfile1", "ls -la /tmp", "file1"},
		{"exit line", "command\nexit\noutput", "command", "output"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(CleanPTYOutput([]byte(tt.input), tt.cmd))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanPTYOutput_gatewayBannersStripped(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"RunPod banner", "-- RUNPOD.IO --\nEnjoy your Pod #abc123 ^_^\noutput", "output"},
		{"connection closed", "data\nConnection to 10.0.0.1 closed.\n", "data"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(CleanPTYOutput([]byte(tt.input), ""))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanPTYOutput_emptyLinesCollapsed(t *testing.T) {
	in := []byte("a\n\n\n\n\nb\n\n\nc\n")
	got := string(CleanPTYOutput(in, ""))
	if got != "a\n\nb\n\nc" {
		t.Errorf("got %q, want %q", got, "a\n\nb\n\nc")
	}
}

func TestCleanPTYOutput_trimmed(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"leading newlines", "\n\n\noutput", "output"},
		{"trailing newlines", "output\n\n\n", "output"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(CleanPTYOutput([]byte(tt.input), ""))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanPTYOutput_realWorldRunPod(t *testing.T) {
	esc := byte(0x1b)
	bel := byte(0x07)
	// Simulates actual PTY output from RunPod exec
	input := []byte("\r\nexit\r\n\r\n-- RUNPOD.IO --\r\nEnjoy your Pod #xyz ^_^\r\n\r\n" +
		string(esc) + "[?2004h" + string(esc) + "]0;root@host" + string(bel) + "root@host:/workspace# hostname\n" +
		string(esc) + "[?2004l\nresult\n" +
		string(esc) + "[?2004h" + string(esc) + "]0;root@host" + string(bel) + "root@host:/workspace# exit\n" +
		string(esc) + "[?2004l\nexit\nConnection to host closed.\r\n")
	got := string(CleanPTYOutput(input, "hostname"))
	if got != "result" {
		t.Errorf("got %q, want %q", got, "result")
	}
}

func TestCleanPTYOutput_emptyInput(t *testing.T) {
	tests := [][]byte{nil, {}}
	for _, in := range tests {
		got := CleanPTYOutput(in, "")
		if got != nil {
			t.Errorf("expected nil for empty input, got %q", got)
		}
	}
}
