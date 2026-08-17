package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every pattern class gets a case. These notes are synced to iCloud and
// committed to a git repo, so a miss here is a secret published to every
// machine on the account.
func TestRedactPatternClasses(t *testing.T) {
	var r Redactor
	cases := []struct{ name, secret string }{
		{"anthropic", "sk-ant-api03-AbCdEf0123456789_deadbeef"},
		// Built by concatenation, not as one literal: GitHub's push-protection
		// scanner matches on shape (prefix + charset + length) with no entropy
		// check, so even an obviously-fake repeated-character literal in this
		// shape gets blocked at push time if it appears whole in the diff.
		{"openrouter", "sk-or-v1-" + strings.Repeat("x", 40)},
		{"openai", "sk-proj0123456789abcdefghijklmnop"},
		{"github-classic", "ghp_0123456789abcdefghijklmnopqrstuvwx"},
		{"github-pat", "github_pat_11ABCDEFG0123456789_abcdefghij"},
		{"slack", "xoxb-123456789012-abcdefghijklm"},
		{"aws", "AKIAIOSFODNN7EXAMPLE"},
		{"google", "AIzaSyA0123456789abcdefghijklmnopqrstu"},
		{"stripe", "sk_live_" + strings.Repeat("x", 24)},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"},
		{"email", "someone@example.com"},
		{"pan", "ABCDE1234F"},
		{"aadhaar", "2345 6789 0123"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := "the value is " + c.secret + " right there"
			got := r.Redact(in)
			if strings.Contains(got, c.secret) {
				t.Errorf("%s survived redaction: %q", c.name, got)
			}
			if !strings.Contains(got, mask) {
				t.Errorf("no mask inserted: %q", got)
			}
		})
	}
}

func TestRedactPEMBlock(t *testing.T) {
	var r Redactor
	in := "before\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA0123\nabcdef\n-----END RSA PRIVATE KEY-----\nafter"
	got := r.Redact(in)
	if strings.Contains(got, "MIIEowIBAAKCAQEA0123") {
		t.Errorf("PEM body survived: %q", got)
	}
	// The surrounding prose is what makes the note useful; only the key goes.
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Errorf("redaction ate the surrounding text: %q", got)
	}
}

// The assignment rule keeps the key visible and masks only the value, so a note
// still records that the session was about an API key.
func TestRedactAssignmentKeepsKey(t *testing.T) {
	var r Redactor
	got := r.Redact("export API_KEY=hunter2supersecretvalue")
	if strings.Contains(got, "hunter2supersecretvalue") {
		t.Errorf("value survived: %q", got)
	}
	if !strings.Contains(got, "API_KEY") {
		t.Errorf("key name should survive for context: %q", got)
	}
}

func TestNewRedactorLoadsLiteralTerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "redactions.txt")
	// A term containing regex metacharacters must be matched literally, not
	// compiled as a pattern.
	body := "# comment\n\nAcme.Corp (Internal)\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewRedactor(path)
	got := r.Redact("we discussed Acme.Corp (Internal) today")
	if strings.Contains(got, "Acme.Corp (Internal)") {
		t.Errorf("literal term not redacted: %q", got)
	}
}

func TestNewRedactorMissingFileIsFine(t *testing.T) {
	r := NewRedactor(filepath.Join(t.TempDir(), "absent.txt"))
	if got := r.Redact("plain text"); got != "plain text" {
		t.Errorf("missing term file changed behaviour: %q", got)
	}
}

// Scrub is the only way to produce a Clean, and Summarize takes only a Clean.
// That is what makes "redact before you summarize" a compile-time property
// rather than a comment — the ordering bug the Python original had.
func TestScrubProducesRedactedClean(t *testing.T) {
	var r Redactor
	c := r.Scrub("token: ghp_0123456789abcdefghijklmnopqrstuvwx")
	if strings.Contains(string(c), "ghp_0123456789abcdefghijklmnopqrstuvwx") {
		t.Errorf("Scrub returned unredacted text: %q", c)
	}
}
