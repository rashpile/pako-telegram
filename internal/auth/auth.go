// Package auth provides chat ID-based authorization for Telegram commands.
package auth

import "sync"

// Authorizer validates whether a chat ID is allowed to execute commands.
type Authorizer interface {
	// IsAllowed returns true if the chat ID is in the allowlist.
	IsAllowed(chatID int64) bool

	// Reload replaces the allowlist with a new set of chat IDs.
	Reload(allowedIDs []int64)
}

// Allowlist implements Authorizer using a set of permitted chat IDs.
type Allowlist struct {
	mu      sync.RWMutex
	allowed map[int64]struct{}
}

// NewAllowlist creates an Authorizer that permits only the specified chat IDs.
func NewAllowlist(allowedIDs []int64) *Allowlist {
	a := &Allowlist{
		allowed: make(map[int64]struct{}, len(allowedIDs)),
	}
	for _, id := range allowedIDs {
		a.allowed[id] = struct{}{}
	}
	return a
}

// IsAllowed returns true if the chat ID is in the allowlist.
func (a *Allowlist) IsAllowed(chatID int64) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, ok := a.allowed[chatID]
	return ok
}

// Reload replaces the allowlist with a new set of chat IDs.
func (a *Allowlist) Reload(allowedIDs []int64) {
	newAllowed := make(map[int64]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		newAllowed[id] = struct{}{}
	}

	a.mu.Lock()
	a.allowed = newAllowed
	a.mu.Unlock()
}
