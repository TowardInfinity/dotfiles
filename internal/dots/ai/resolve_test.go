package ai

import (
	"reflect"
	"testing"

	"github.com/TowardInfinity/dotfiles/internal/dots/memory"
)

func TestResolveLaunch(t *testing.T) {
	project := memory.ProjectKey("github.com/towardinfinity/dotfiles")
	tests := []struct {
		name string
		tool string
		args []string
		want []string
		err  string
	}{
		{
			name: "claude resumes with no trailing arguments",
			tool: "claude",
			want: []string{"claude", "--continue"},
		},
		{
			name: "claude preserves a prompt",
			tool: "claude",
			args: []string{"continue the deployment work"},
			want: []string{"claude", "--continue", "continue the deployment work"},
		},
		{
			name: "codex uses the cwd scoped last flag",
			tool: "codex",
			want: []string{"codex", "resume", "--last"},
		},
		{
			name: "codex preserves extra flags and prompt",
			tool: "codex",
			args: []string{"--model", "gpt-5.4", "finish the review"},
			want: []string{"codex", "resume", "--last", "--model", "gpt-5.4", "finish the review"},
		},
		{
			name: "grok resumes with native continue",
			tool: "grok",
			want: []string{"grok", "--continue"},
		},
		{
			name: "cursor receives its explicit workspace before resume",
			tool: "cursor",
			args: []string{"--workspace", "/work/dotfiles"},
			want: []string{"cursor-agent", "--workspace", "/work/dotfiles", "resume"},
		},
		{
			name: "cursor keeps user arguments after resume",
			tool: "cursor",
			args: []string{"--workspace", "/work/dotfiles", "--continue"},
			want: []string{"cursor-agent", "--workspace", "/work/dotfiles", "resume", "--continue"},
		},
		{
			name: "unknown tool is absent from the static table",
			tool: "chatgpt",
			err:  `unknown AI tool "chatgpt"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveLaunch(test.tool, project, test.args)
			if test.err != "" {
				if err == nil || err.Error() != test.err {
					t.Fatalf("error = %v, want %q", err, test.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("argv = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestResolveLaunchDoesNotChangeWithProjectKey(t *testing.T) {
	first, err := ResolveLaunch("codex", memory.ProjectKey("github.com/a/one"), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveLaunch("codex", memory.ProjectKey("github.com/b/two"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("phase-1 native argv changed with project: %#v vs %#v", first, second)
	}
}
