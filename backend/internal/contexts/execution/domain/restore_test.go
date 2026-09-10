package domain

import (
	"testing"
	"time"
)

func TestRestoreValidatesStoredStateWithoutExposingMutation(t *testing.T) {
	at := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	r, err := NewQueuedRun("run_1", "ws_a", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start(at); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot()
	restored, err := Restore(s)
	if err != nil || restored.Snapshot() != s {
		t.Fatal(err)
	}
	if _, err = restored.RequestCancel(at); err != nil || r.Snapshot().State != Running {
		t.Fatal("restored value aliased storage", err)
	}
	for _, change := range []func(*Snapshot){func(s *Snapshot) { s.ID = "../run" }, func(s *Snapshot) { s.Version = 0 }, func(s *Snapshot) { s.State = "invented" }, func(s *Snapshot) { s.UpdatedAt = at.Add(-time.Second) }, func(s *Snapshot) { s.State = Queued }, func(s *Snapshot) { s.Version = 1 }} {
		invalid := s
		change(&invalid)
		if _, err := Restore(invalid); err == nil {
			t.Fatal("corrupt snapshot accepted", invalid)
		}
	}
}
