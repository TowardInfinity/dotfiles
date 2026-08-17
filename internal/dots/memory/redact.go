package memory

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Redaction masks secrets before a transcript leaves this process.
//
// Ordering is the point. The Python original redacted only the finished note,
// after sending the raw transcript to the summarizer — so a key could be
// reworded by the model into a form the regexes no longer match and land in the
// vault anyway. Here Redact runs on the transcript first, and the summarizer
// only ever sees masked text. The vault is synced to iCloud and backed by git;
// what reaches it is effectively published to every machine on the account.
//
// These patterns are ported from ~/.claude/hooks/save_to_obsidian.py.
var builtinPatterns = []*regexp.Regexp{
	// Provider API keys, by documented prefix. Anthropic before the generic
	// sk- rules: anthropic, openrouter, openai. Order matters — the longer,
	// more specific match wins.
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{10,}\b`),
	regexp.MustCompile(`\bsk-or-v1-[A-Za-z0-9_-]{20,}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
	regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`\bxox[a-z]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{30,}\b`),
	regexp.MustCompile(`\b(?:sk|pk|rk)_(?:live|test)_[A-Za-z0-9]{20,}\b`),
	// JWTs — header.payload.signature, all base64url.
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
	// PEM private key blocks. (?s) so . spans the newlines of the body.
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	// Email addresses.
	regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`),
	// Indian PAN (ABCDE1234F) and Aadhaar (never starts with 0 or 1).
	regexp.MustCompile(`\b[A-Z]{5}[0-9]{4}[A-Z]\b`),
	regexp.MustCompile(`\b[2-9]\d{3}\s\d{4}\s\d{4}\b`),
}

// assignmentPattern masks the value of a secret-looking assignment while
// keeping the key visible: "api_key = [redacted]" still tells a human what the
// session was about, which is the whole point of the note.
var assignmentPattern = regexp.MustCompile(`(?i)\b(password|passwd|secret|api[_-]?key|token)(\s*[=:]\s*)\S+`)

const mask = "[redacted]"

// Redactor masks secrets. The zero value works and applies only the built-in
// patterns; NewRedactor adds the user's own terms.
type Redactor struct {
	terms []*regexp.Regexp
}

// NewRedactor loads extra literal terms from redactions.txt — one per line,
// # for comments. This is where a hostname, an employer name or an internal
// project codename goes: things no pattern can infer but which should not be
// in a synced vault. A missing file is normal and not an error.
func NewRedactor(path string) *Redactor {
	r := &Redactor{}
	f, err := os.Open(path)
	if err != nil {
		return r
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// QuoteMeta: these are literal strings, and an unescaped one
		// containing regex metacharacters would either fail to compile or
		// silently match the wrong thing.
		r.terms = append(r.terms, regexp.MustCompile(`(?i)`+regexp.QuoteMeta(line)))
	}
	return r
}

// DefaultRedactionsPath is where install.sh puts the term list.
func DefaultRedactionsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "hooks", "redactions.txt")
}

// Clean is text that has been through the redactor.
//
// Summarize and the vault writer accept only this type, so "redact before you
// summarize" is a thing the compiler checks rather than a comment someone has
// to remember. Scrub is the only way to obtain one — the ordering bug in the
// Python original cannot be reintroduced without deliberately casting.
type Clean string

// Scrub redacts s and marks it as safe to leave the process.
func (r *Redactor) Scrub(s string) Clean { return Clean(r.Redact(s)) }

// Redact masks every known secret shape in s.
func (r *Redactor) Redact(s string) string {
	for _, p := range builtinPatterns {
		s = p.ReplaceAllString(s, mask)
	}
	s = assignmentPattern.ReplaceAllString(s, "${1}${2}"+mask)
	for _, t := range r.terms {
		s = t.ReplaceAllString(s, mask)
	}
	return s
}
