package provider

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/filfreire/idlebat/internal/config"
)

func TestBuildCommand(t *testing.T) {
	tests := []struct {
		provider    string
		binary      string
		contains    []string
		notContains []string
	}{
		{
			"claude",
			"claude",
			[]string{"-p", "--dangerously-skip-permissions", "stream-json", "test-model"},
			[]string{"--permission-mode"},
		},
		{
			"codex",
			"codex",
			[]string{"exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "test-model", "-"},
			[]string{"--sandbox", "workspace-write"},
		},
		{
			"cursor",
			"agent",
			[]string{"-p", "--force", "--sandbox", "disabled", "--approve-mcps", "--trust", "stream-json", "test-model"},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			step := config.ExpandedStep{Provider: tt.provider, Model: "test-model"}
			cmd, err := BuildCommand(step, "/work", "/artifacts", "hello")
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(cmd.Path) != tt.binary {
				t.Fatalf("path = %q, want binary %q", cmd.Path, tt.binary)
			}
			for _, expected := range tt.contains {
				found := false
				for _, arg := range cmd.Args {
					if arg == expected {
						found = true
					}
				}
				if !found {
					t.Errorf("args %v do not contain %q", cmd.Args, expected)
				}
			}
			for _, unexpected := range tt.notContains {
				for _, arg := range cmd.Args {
					if arg == unexpected {
						t.Errorf("args %v unexpectedly contain %q", cmd.Args, unexpected)
					}
				}
			}
		})
	}
}

func TestBuildCommandRejectsUnknownProvider(t *testing.T) {
	_, err := BuildCommand(config.ExpandedStep{Provider: "other"}, "/work", "/artifacts", "hello")
	if err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestFinalText(t *testing.T) {
	tests := []struct {
		provider string
		line     string
		want     string
	}{
		{"claude", `{"type":"result","result":"done"}`, "done"},
		{"claude", `{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`, "working"},
		{"cursor", `{"type":"result","message":{"content":[{"type":"text","text":"cursor done"}]}}`, "cursor done"},
		{"codex", `{"type":"item.completed","item":{"type":"agent_message","text":"codex done"}}`, "codex done"},
		{"codex", `{"type":"turn.started"}`, ""},
		{"claude", `not-json`, ""},
	}
	for _, tt := range tests {
		if got := FinalText(tt.provider, tt.line); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("FinalText(%q, %q) = %q, want %q", tt.provider, tt.line, got, tt.want)
		}
	}
}
