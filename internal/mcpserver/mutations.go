package mcpserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MutationLogger appends one JSON record per write tool invocation to a file
// at <root>/log/mutations.jsonl. It is safe for concurrent use.
//
// The schema is deliberately open: callers pass any JSON-serialisable args
// map; we add `ts`, `tool` and `graph_id` fields. Future ticket #56 (multi-
// graph + branch-aware ops) will extend this; for now the record shape is:
//
//	{"ts":"2026-...","tool":"graph_add_node","graph_id":"foo","args":{...},"result":{...}}
type MutationLogger struct {
	path string
	mu   sync.Mutex
	now  func() time.Time // injected for tests
}

// NewMutationLogger constructs a logger that writes to <root>/log/mutations.jsonl.
// The directory is created if missing on first append.
func NewMutationLogger(root string) *MutationLogger {
	return &MutationLogger{
		path: filepath.Join(root, "log", "mutations.jsonl"),
		now:  time.Now,
	}
}

// Path returns the log file path; useful for tests and operational debugging.
func (l *MutationLogger) Path() string { return l.path }

// MutationRecord is the JSONL line format. Exposed so tests can decode it.
type MutationRecord struct {
	Timestamp string         `json:"ts"`
	Tool      string         `json:"tool"`
	GraphID   string         `json:"graph_id"`
	Args      map[string]any `json:"args,omitempty"`
	Result    map[string]any `json:"result,omitempty"`
}

// Append writes one record. Caller owns the record fields; ts is filled in.
func (l *MutationLogger) Append(rec MutationRecord) (retErr error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("mcpserver.MutationLogger: mkdir: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("mcpserver.MutationLogger: open: %w", err)
	}
	defer func() {
		// Surface Close errors only when nothing else failed; Close on a
		// write-path can flush buffered-write failures that Write missed.
		if cerr := f.Close(); cerr != nil && retErr == nil {
			retErr = fmt.Errorf("mcpserver.MutationLogger: close: %w", cerr)
		}
	}()

	if rec.Timestamp == "" {
		rec.Timestamp = l.now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("mcpserver.MutationLogger: marshal: %w", err)
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("mcpserver.MutationLogger: write: %w", err)
	}
	return nil
}
