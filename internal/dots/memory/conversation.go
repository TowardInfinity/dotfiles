package memory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Conversation renders a session's transcript as plain dialogue for the
// summarizer.
//
// It applies the same filters the adapters use for counting — no sidechains, no
// tool results, no injected context — because the summarizer is even more
// sensitive to them than the counter was: handed a session's worth of tool
// output, a 12B model writes a summary of the tool output.
//
// Returns the text, the number of genuine user turns, and an error. Tools whose
// adapters supply their own summary have no reader here and report that
// plainly; the caller treats it as "nothing to distill", not as a failure.
func Conversation(s Session) (text string, userMsgs int, err error) {
	switch s.Tool {
	case "claude-code":
		return claudeConversation(s.Transcript)
	case "codex":
		return codexConversation(s.Transcript)
	case "cursor":
		return cursorConversation(s.Transcript)
	}
	return "", 0, fmt.Errorf("no transcript reader for %s", s.Tool)
}

// convo accumulates dialogue turns and caps total size while reading, so a
// 40 MB transcript is never fully in memory just to keep its last 24 000 chars.
type convo struct {
	b     strings.Builder
	users int
}

func (c *convo) add(role, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if role == "user" {
		c.users++
	}
	c.b.WriteString("## " + role + "\n")
	c.b.WriteString(text)
	c.b.WriteString("\n\n")

	// Keep only a working tail. The summarizer truncates to the tail anyway;
	// doing it here bounds memory during the read.
	const softCap = 4 * maxTranscriptChars
	if c.b.Len() > softCap {
		s := c.b.String()
		c.b.Reset()
		c.b.WriteString(s[len(s)-maxTranscriptChars*2:])
	}
}

func claudeConversation(path string) (string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	var c convo
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)

	for sc.Scan() {
		var line claudeLine
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		if line.IsSidechain || (line.Type != "user" && line.Type != "assistant") {
			continue
		}
		text := contentText(line.Message.Content)
		if line.Type == "user" {
			if len(line.ToolUseResult) > 0 || IsInjected(text) {
				continue
			}
			c.add("user", text)
			continue
		}
		c.add("assistant", stripFenced(text))
	}
	return c.b.String(), c.users, sc.Err()
}

// cursorConversation reads the flat JSON array of user prompts
// CursorAdapter's Transcript points at. There is no assistant side to read —
// see adapter_cursor.go for why — so this is the only reader here that
// cannot show the summarizer both halves of a conversation. It says so up
// front, in the text itself, rather than leaving the summarizer to guess:
// handed a page of nothing but user turns, an unwarned model tends to invent
// a plausible-sounding reply to go with them.
func cursorConversation(path string) (string, int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return "", 0, err
	}

	var c convo
	c.add("note", "Only this session's user prompts were recoverable — Cursor "+
		"stores the assistant's replies encrypted on disk. Summarize what the "+
		"user asked for; do not describe or assume what the assistant did in "+
		"response.")
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil && s != "" {
			c.add("user", s)
		}
	}
	return c.b.String(), c.users, nil
}

func codexConversation(path string) (string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	var c convo
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)

	for sc.Scan() {
		var line codexLine
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		if line.Type != "response_item" || line.Payload.PayloadType != "message" {
			continue
		}
		role := line.Payload.Role
		if role != "user" && role != "assistant" {
			continue
		}
		var parts []string
		for _, p := range line.Payload.Content {
			if p.Text != "" {
				parts = append(parts, p.Text)
			}
		}
		text := strings.Join(parts, "\n")
		if role == "user" && IsInjected(text) {
			continue
		}
		c.add(role, stripFenced(text))
	}
	return c.b.String(), c.users, sc.Err()
}
