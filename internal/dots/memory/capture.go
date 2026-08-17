package memory

import (
	"context"
	"os"
	"sort"
	"time"
)

// Capture is what the Stop hook runs. Its cadence is the whole design problem.
//
// Stop fires at the end of every assistant turn, not once per session. The
// Python hook treated it as end-of-session and re-sent the full transcript to
// Ollama every single turn — which is how the vault ended up with 70 notes for
// 39 sessions, each one rewritten dozens of times.
//
// So capture does no expensive work on the session you are in. It records
// metadata and leaves. Distillation happens only for sessions whose transcript
// has stopped moving, which has three properties worth having: a live session
// is never summarized mid-thought, the summary is written once, and the session
// you are currently in does not need a note anyway — you are still in it.
//
// The same pass also picks up Codex and Grok. Neither tool has a hook, so
// without this they would enter the index only when someone remembered to run
// `dots memory reindex` by hand, and "memory across all my tools" would quietly
// decay to "memory of whichever tool I last reindexed".
const (
	// defaultIdle is how long a transcript must be untouched before it counts
	// as finished. Long enough to cover thinking time and a slow tool call.
	defaultIdle = 20 * time.Minute

	// defaultScanInterval bounds how often the full cross-tool scan runs.
	// Scanning every adapter takes a couple of seconds; doing that on every
	// assistant turn is exactly the mistake being fixed here.
	defaultScanInterval = 30 * time.Minute

	// defaultMaxDistill caps Ollama calls per pass, so a first run against a
	// large backlog spreads over several passes instead of pinning the GPU for
	// an hour the first time the hook fires.
	defaultMaxDistill = 3

	// maxExamine caps how many transcripts a pass reads off disk, separately
	// from how many it summarizes. The distill cap only counts successful
	// Ollama calls, so on its own it lets an unbounded number of sessions be
	// parsed and discarded as too slight — and a Codex rollout runs to tens of
	// thousands of lines. This is the bound that actually holds.
	maxExamine = 40
)

// CaptureOptions configures one capture pass. The zero value is sensible.
type CaptureOptions struct {
	Idle         time.Duration
	ScanInterval time.Duration
	Max          int
	Vault        string
	Experimental bool

	// DryRun does everything except write notes and the index.
	DryRun bool

	// Force ignores the idle and scan-interval gates. This is what `dots memory
	// reindex` uses; it must never be the hook's behaviour.
	Force bool
}

func (o *CaptureOptions) applyDefaults() {
	if o.Idle == 0 {
		o.Idle = defaultIdle
	}
	if o.ScanInterval == 0 {
		o.ScanInterval = defaultScanInterval
	}
	if o.Max == 0 {
		o.Max = defaultMaxDistill
	}
	if o.Vault == "" {
		o.Vault = VaultDir()
	}
	if o.Force {
		o.Idle, o.ScanInterval = 0, 0
	}
}

// CaptureReport says what a pass did, so the CLI can show it and the hook can
// stay silent.
type CaptureReport struct {
	Scanned    bool
	Indexed    int
	Distilled  int
	NotesWrote int
	Skipped    int
	Errs       []error

	// Locked means another pass was already running and this one did nothing.
	Locked bool

	Reconciled ReconcileReport

	// ProjectIndexes is how many "AI Chats/Projects/<project>.md" Dataview
	// pages were (re)written this pass. Zero on a dry run or a pass with no
	// vault configured — those never call WriteProjectIndexes at all.
	ProjectIndexes int
}

// Run performs one capture pass.
//
// It returns an error only for conditions the caller could act on. Everything
// else — no Ollama, no vault, an unreadable transcript — is recorded and
// stepped over, because this runs from a hook where failing loudly means
// interrupting the user's session over a background bookkeeping task.
func Run(ctx context.Context, opts CaptureOptions) (CaptureReport, error) {
	opts.applyDefaults()

	var rep CaptureReport

	// Single-flight. A dry run reads only, so it needs no lock and must not be
	// blocked by a real pass holding one.
	if !opts.DryRun {
		release, ok := acquireLock()
		if !ok {
			rep.Locked = true
			return rep, nil
		}
		defer release()
	}

	idx := LoadIndex()
	red := NewRedactor(DefaultRedactionsPath())

	if opts.Force || time.Since(idx.Built) >= opts.ScanInterval {
		for _, r := range ScanAll(&idx, red, opts.Experimental) {
			if r.Err != nil {
				rep.Errs = append(rep.Errs, r.Err)
			}
		}
		rep.Scanned = true

		// Only after a scan, so the index knows about every session the vault
		// might hold a note for. Skipped on a dry run because it moves files.
		if opts.Vault != "" && !opts.DryRun {
			rep.Reconciled = Reconcile(opts.Vault, &idx)
			rep.Errs = append(rep.Errs, rep.Reconciled.Errs...)

			// Project index pages are regenerated whole, same as Reconcile:
			// cheap, and only meaningful once the index reflects this pass's
			// scan. A write failure here is recorded, not fatal — a stale or
			// missing Dataview page is a much smaller problem than a capture
			// pass that stops indexing sessions over it.
			n, err := WriteProjectIndexes(opts.Vault, idx)
			rep.ProjectIndexes = n
			if err != nil {
				rep.Errs = append(rep.Errs, err)
			}
		}
	}
	rep.Indexed = len(idx.Sessions)

	now := time.Now()
	var due []int
	for i, s := range idx.Sessions {
		if noteDue(s, now, opts.Idle) {
			due = append(due, i)
		}
	}
	// Newest first: if the cap bites, recent work is what you are most likely
	// to want recalled.
	sort.SliceStable(due, func(a, b int) bool {
		return idx.Sessions[due[a]].Updated.After(idx.Sessions[due[b]].Updated)
	})

	var sum *Summarizer
	var examined int
	for _, i := range due {
		s := &idx.Sessions[i]

		// A session that already carries a summary (Grok supplies its own, and
		// reconciliation adopts one from an existing note) only needs its note
		// written — no transcript read, no Ollama call, so neither budget
		// applies to it.
		if s.Summary == "" {
			if rep.Distilled >= opts.Max || examined >= maxExamine {
				rep.Skipped++
				continue
			}
			examined++

			text, users, err := Conversation(*s)
			if err != nil {
				rep.Skipped++
				continue
			}
			clean := red.Scrub(text)
			if !WorthSummarizing(users, clean) {
				// Too slight to be worth a note. Record that it was examined at
				// this size, so the next pass skips it without reading the
				// transcript again — an empty summary alone cannot distinguish
				// this from "not looked at yet".
				s.Trivial = true
				s.CapturedBytes = transcriptSize(*s)
				rep.Skipped++
				continue
			}
			if sum == nil {
				sum = NewSummarizer()
				if !sum.Available(ctx) {
					// No Ollama on this box. Metadata is still indexed and
					// recall still works on titles; notes fill in later.
					break
				}
			}
			body, err := sum.Summarize(ctx, clean)
			if err != nil {
				rep.Errs = append(rep.Errs, err)
				continue
			}
			s.Summary = body
			s.Trivial = false
			rep.Distilled++
		}

		if opts.Vault != "" && !opts.DryRun {
			path, err := WriteNote(opts.Vault, *s, red.Scrub(s.Summary))
			if err != nil {
				rep.Errs = append(rep.Errs, err)
				continue
			}
			s.VaultNote = path
			rep.NotesWrote++
		}
		s.CapturedBytes = transcriptSize(*s)
	}

	if opts.DryRun {
		return rep, nil
	}
	return rep, SaveIndex(idx)
}

// noteDue reports whether a session needs distillation now.
//
// The idle check is what replaces a debounce on transcript growth. A byte-delta
// debounce still summarizes a long session ten or twenty times; waiting for the
// transcript to go quiet summarizes it once, and works the same whether the
// trigger was Stop, SessionEnd or a manual reindex.
func noteDue(s Session, now time.Time, idle time.Duration) bool {
	if s.ID == "" || s.Messages == 0 {
		return false
	}
	// Still moving — this is very likely the session that just invoked us.
	if now.Sub(s.Updated) < idle {
		return false
	}
	// Grown (or shrunk, or never looked at) since the last pass. This has to be
	// tested before any verdict below, because a verdict is only about the
	// transcript as it stood at CapturedBytes.
	if s.CapturedBytes != transcriptSize(s) {
		return true
	}
	// Read at this size and judged not worth a note. Testing an empty Summary
	// first — as this did originally — makes the trivial case due forever, so
	// every abandoned session is re-read from disk on every pass.
	if s.Trivial {
		return false
	}
	if s.Summary == "" {
		return true
	}
	// Summary exists but its note was never written (no vault at the time, or
	// the note was deleted from the vault by hand).
	if s.VaultNote == "" {
		return true
	}
	if _, err := os.Stat(s.VaultNote); err != nil {
		return true
	}
	return false
}

func transcriptSize(s Session) int64 {
	st, err := os.Stat(s.Transcript)
	if err != nil {
		return 0
	}
	return st.Size()
}
