package memory

import "testing"

// The single most important property: the same repository reached by different
// URL spellings must produce one key. Otherwise a clone over SSH and a clone
// over HTTPS are two different memories of the same project.
func TestNormalizeRemoteCollapsesSpellings(t *testing.T) {
	same := []string{
		"git@github.com:TowardInfinity/almanac.git",
		"git@github.com:TowardInfinity/almanac",
		"https://github.com/TowardInfinity/almanac.git",
		"https://github.com/TowardInfinity/almanac",
		"ssh://git@github.com/TowardInfinity/almanac.git",
		"https://TowardInfinity@github.com/TowardInfinity/almanac.git",
	}
	const want = "github.com/TowardInfinity/almanac"
	for _, r := range same {
		if got := NormalizeRemote(r); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", r, got, want)
		}
	}
}

func TestNormalizeRemoteOtherHosts(t *testing.T) {
	cases := map[string]string{
		"git@gitlab.com:group/sub/proj.git":      "gitlab.com/group/sub/proj",
		"https://bitbucket.org/team/repo.git":    "bitbucket.org/team/repo",
		"git@ssh.dev.azure.com:v3/org/proj/repo": "ssh.dev.azure.com/v3/org/proj/repo",
		"":                                       "",
	}
	for in, want := range cases {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

// A directory outside any repository still needs a key, and it must be stable
// rather than empty — sessions there are not lost, only unscoped to a repo.
func TestResolveProjectNoGit(t *testing.T) {
	dir := t.TempDir()
	key, gitRoot, remote := ResolveProject(dir)
	if key == "" {
		t.Fatal("ResolveProject returned an empty key for a non-repo directory")
	}
	if gitRoot != "" || remote != "" {
		t.Errorf("non-repo dir reported gitRoot=%q remote=%q", gitRoot, remote)
	}
	// Stable across calls, including through the memoization cache.
	if again, _, _ := ResolveProject(dir); again != key {
		t.Errorf("ResolveProject not stable: %q then %q", key, again)
	}
}

func TestResolveProjectEmptyDir(t *testing.T) {
	if key, _, _ := ResolveProject(""); key != Unscoped {
		t.Errorf("ResolveProject(\"\") = %q, want %q", key, Unscoped)
	}
}
