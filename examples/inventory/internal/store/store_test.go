package store

import "testing"

func TestAdd(t *testing.T) {
	s := New()
	if err := s.Add("bolt", 3); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("bolt", -4); err != ErrNotEnough {
		t.Fatalf("got %v, want ErrNotEnough", err)
	}
	if got := s.Count("bolt"); got != 3 {
		t.Fatalf("count %d, want 3", got)
	}
}
