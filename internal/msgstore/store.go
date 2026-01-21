// Package msgstore provides persistent storage for sent message IDs.
// Used to track file messages for later cleanup.
package msgstore

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Entry represents a stored message.
type Entry struct {
	ChatID    int64     `json:"chat_id"`
	MessageID int       `json:"message_id"`
	SentAt    time.Time `json:"sent_at"`
}

// Store manages persistent storage of sent message IDs.
type Store struct {
	mu      sync.RWMutex
	path    string
	entries []Entry
}

// New creates a new message store.
// If path is empty, the store operates in memory-only mode (no persistence).
func New(path string) (*Store, error) {
	s := &Store{path: path}
	if path != "" {
		if err := s.load(); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return s, nil
}

// Add stores a new message entry.
func (s *Store) Add(chatID int64, messageID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append(s.entries, Entry{
		ChatID:    chatID,
		MessageID: messageID,
		SentAt:    time.Now(),
	})

	return s.save()
}

// AddBatch stores multiple message entries at once.
func (s *Store) AddBatch(chatID int64, messageIDs []int) error {
	if len(messageIDs) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, msgID := range messageIDs {
		s.entries = append(s.entries, Entry{
			ChatID:    chatID,
			MessageID: msgID,
			SentAt:    now,
		})
	}

	return s.save()
}

// GetByTimeRange returns entries within the specified time range.
func (s *Store) GetByTimeRange(chatID int64, from, to time.Time) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Entry
	for _, e := range s.entries {
		if e.ChatID == chatID && !e.SentAt.Before(from) && e.SentAt.Before(to) {
			result = append(result, e)
		}
	}
	return result
}

// GetAll returns all entries for a chat.
func (s *Store) GetAll(chatID int64) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Entry
	for _, e := range s.entries {
		if e.ChatID == chatID {
			result = append(result, e)
		}
	}
	return result
}

// GetBefore returns entries sent before the specified time.
func (s *Store) GetBefore(chatID int64, before time.Time) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Entry
	for _, e := range s.entries {
		if e.ChatID == chatID && e.SentAt.Before(before) {
			result = append(result, e)
		}
	}
	return result
}

// GetAfter returns entries sent after the specified time.
func (s *Store) GetAfter(chatID int64, after time.Time) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Entry
	for _, e := range s.entries {
		if e.ChatID == chatID && e.SentAt.After(after) {
			result = append(result, e)
		}
	}
	return result
}

// Remove deletes entries by message IDs.
func (s *Store) Remove(chatID int64, messageIDs []int) error {
	if len(messageIDs) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Build lookup set
	toRemove := make(map[int]bool)
	for _, id := range messageIDs {
		toRemove[id] = true
	}

	// Filter out removed entries
	var remaining []Entry
	for _, e := range s.entries {
		if e.ChatID != chatID || !toRemove[e.MessageID] {
			remaining = append(remaining, e)
		}
	}

	s.entries = remaining
	return s.save()
}

// Count returns the number of stored entries for a chat.
func (s *Store) Count(chatID int64) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, e := range s.entries {
		if e.ChatID == chatID {
			count++
		}
	}
	return count
}

// Enabled returns true if the store has persistence enabled.
func (s *Store) Enabled() bool {
	return s.path != ""
}

// load reads entries from the persistent file.
func (s *Store) load() error {
	if s.path == "" {
		return nil
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &s.entries)
}

// save writes entries to the persistent file.
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}

	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0644)
}