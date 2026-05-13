package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestReadMissingIDReturnsJSONError(t *testing.T) {
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"read"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}

	var got map[string]any
	if json.Unmarshal(out.Bytes(), &got) != nil {
		t.Fatalf("expected json output, got %q", out.String())
	}
	if got["ok"] != false || got["error"] != "invalid_args" || got["message"] != "id required" {
		t.Fatalf("unexpected response: %#v", got)
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name string
		data string
		want int
	}{
		{name: "empty", data: "", want: 0},
		{name: "trailing newline", data: "a\nb\n", want: 2},
		{name: "non trailing final line", data: "a\nb", want: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := countLines([]byte(test.data)); got != test.want {
				t.Fatalf("countLines(%q) = %d, want %d", test.data, got, test.want)
			}
		})
	}
}
