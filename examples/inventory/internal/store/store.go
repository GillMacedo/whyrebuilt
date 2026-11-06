// Package store keeps item counts in memory.
package store

import (
	"errors"
	"sort"
	"sync"
)

// ErrNotEnough is returned when a removal would make a count negative.
var ErrNotEnough = errors.New("not enough items")

// Store holds counts by item name.
type Store struct {
	mu     sync.Mutex
	counts map[string]int
}

// New returns an empty store.
func New() *Store { return &Store{counts: map[string]int{}} }

// Add changes the count of an item by n, which may be negative.
func (s *Store) Add(item string, n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counts[item]+n < 0 {
		return ErrNotEnough
	}
	s.counts[item] += n
	return nil
}

// Count returns the count of an item.
func (s *Store) Count(item string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[item]
}

// Items returns the item names in order.
func (s *Store) Items() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.counts))
	for k := range s.counts {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
