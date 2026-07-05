// Package store persists two things:
//
//  1. The idempotency ledger (spec §6): before videos.insert runs, the
//     caller's idempotency key is checked here; a matching successful upload
//     returns the existing result instead of re-publishing.
//  2. The append-only audit trail (spec §8): since analytics is out of scope
//     for v1, this log is the only record of what happened.
//
// Both live under the configured state directory: idempotency.json (atomic
// snapshot) and audit.log (JSONL, one entry per attempt).
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record statuses.
const (
	StatusSuccess    = "success"
	StatusRolledBack = "rolled_back"
)

// Record is one entry in the idempotency ledger, keyed by idempotency key.
type Record struct {
	IdempotencyKey string    `json:"idempotency_key"`
	Channel        string    `json:"channel"`
	Status         string    `json:"status"`
	VideoID        string    `json:"video_id,omitempty"`
	VideoURL       string    `json:"video_url,omitempty"`
	ContentSHA256  string    `json:"content_sha256,omitempty"`
	Title          string    `json:"title,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// AuditEntry is one line of the audit trail (spec §8): channel, timestamp,
// outcome; on failure a categorized reason + the stage at which it failed;
// on success the resulting video ID/URL.
type AuditEntry struct {
	Timestamp       time.Time `json:"timestamp"`
	Channel         string    `json:"channel"`
	Outcome         string    `json:"outcome"` // success | deduplicated | failure | rolled_back
	Stage           string    `json:"stage,omitempty"`
	Category        string    `json:"category,omitempty"`
	Error           string    `json:"error,omitempty"`
	VideoID         string    `json:"video_id,omitempty"`
	VideoURL        string    `json:"video_url,omitempty"`
	OrphanedVideoID string    `json:"orphaned_video_id,omitempty"`
	IdempotencyKey  string    `json:"idempotency_key,omitempty"`
	DurationMS      int64     `json:"duration_ms,omitempty"`
}

type persisted struct {
	LastUploadAt time.Time          `json:"last_upload_at"`
	Records      map[string]*Record `json:"records"`
}

// Store is safe for concurrent use within a single server process.
type Store struct {
	mu       sync.Mutex
	dir      string
	data     persisted
	inFlight map[string]bool
}

// Open loads (or initializes) the ledger under dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create state dir %s: %w", dir, err)
	}
	s := &Store{
		dir:      dir,
		inFlight: map[string]bool{},
		data:     persisted{Records: map[string]*Record{}},
	}
	b, err := os.ReadFile(s.ledgerPath())
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &s.data); err != nil {
			return nil, fmt.Errorf("parse %s (delete/repair it to reset the ledger): %w", s.ledgerPath(), err)
		}
		if s.data.Records == nil {
			s.data.Records = map[string]*Record{}
		}
	case os.IsNotExist(err):
		// fresh state dir
	default:
		return nil, fmt.Errorf("read %s: %w", s.ledgerPath(), err)
	}
	return s, nil
}

func (s *Store) ledgerPath() string { return filepath.Join(s.dir, "idempotency.json") }
func (s *Store) auditPath() string  { return filepath.Join(s.dir, "audit.log") }

// GetSuccess returns the prior successful upload for key, if any (spec §6).
func (s *Store) GetSuccess(key string) (*Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.data.Records[key]
	if !ok || r.Status != StatusSuccess {
		return nil, false
	}
	cp := *r
	return &cp, true
}

// FindSuccessByHash returns all successful uploads whose content hash
// matches — used for the cross-channel duplicate-content guard (spec §5.3).
func (s *Store) FindSuccessByHash(sha string) []*Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Record
	if sha == "" {
		return out
	}
	for _, r := range s.data.Records {
		if r.Status == StatusSuccess && r.ContentSHA256 == sha {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out
}

// BeginInFlight reserves key for the duration of an upload attempt so a
// concurrent retry with the same key cannot race into a double publish.
// Returns false if the key is already in flight.
func (s *Store) BeginInFlight(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFlight[key] {
		return false
	}
	s.inFlight[key] = true
	return true
}

// EndInFlight releases a key reserved by BeginInFlight.
func (s *Store) EndInFlight(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inFlight, key)
}

// LastUploadAt returns the time of the most recent successful upload across
// all channels (used by the timing-clustering guard, spec §5.3).
func (s *Store) LastUploadAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.LastUploadAt
}

// SaveResult upserts a ledger record and persists the snapshot atomically.
func (s *Store) SaveResult(r *Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *r
	s.data.Records[r.IdempotencyKey] = &cp
	if r.Status == StatusSuccess {
		s.data.LastUploadAt = time.Now().UTC()
	}
	return s.persistLocked()
}

func (s *Store) persistLocked() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.ledgerPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.ledgerPath())
}

// Audit appends one JSONL entry to the audit trail (spec §8).
func (s *Store) Audit(e *AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.auditPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}
