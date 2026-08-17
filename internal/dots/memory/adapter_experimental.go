package memory

import (
	"os"
	"path/filepath"
)

// ChatGPTAdapter would read the ChatGPT desktop app's local conversation
// store. It does not, and the reason is evidence, not a deferral: every
// conversation file under
// ~/Library/Application Support/com.openai.chat/conversations-v3-*/*.data
// inspected while building this adapter is high-entropy from byte zero — no
// JSON, no plist, no SQLite header, nothing `file(1)` can pin down twice in a
// row (it guesses "OpenPGP Public Key", "OpenPGP Secret Key" and "DOS
// executable" across siblings in the same directory, which is what its
// heuristics do when handed ciphertext rather than a real, consistent
// format). That is what encrypted-at-rest content looks like, not an
// undocumented format waiting to be reverse-engineered.
//
// Available() still reports honestly — the directory exists on this machine —
// so `dots memory status` shows the tool as present rather than silently
// absent. Scan() returns nothing because there is no parse to write: doing so
// would mean reverse-engineering the app's key handling, which is out of
// scope for this project. If ChatGPT desktop ever ships a documented export,
// this is where it plugs in.
type ChatGPTAdapter struct{}

func (ChatGPTAdapter) Name() string { return "chatgpt" }

func (ChatGPTAdapter) Available() bool {
	_, err := os.Stat(filepath.Join(home(), "Library", "Application Support", "com.openai.chat"))
	return err == nil
}

func (ChatGPTAdapter) Scan() ([]Session, error) { return nil, nil }
