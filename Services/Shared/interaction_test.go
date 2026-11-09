package Shared

import (
	"testing"
)

func TestInteractionStoreBeginBusyAndEnd(t *testing.T) {
	var s InteractionStore[*struct{}]
	req := &struct{}{}
	if !s.Begin("agent1", req, "id1") {
		t.Fatal("first Begin should park the request")
	}
	if s.Begin("agent1", &struct{}{}, "id2") {
		t.Fatal("Begin for the same agent must fail while one is live")
	}
	if got, ok := s.Get("id1"); !ok || got != req {
		t.Fatal("Get must return the parked request")
	}
	if got, ok := s.Live("agent1"); !ok {
		_ = got
	} else if got != req {
		t.Fatal("Live must return the parked request")
	}
	if list := s.List("agent1"); len(list) != 1 {
		t.Fatalf("List should hold the one live request, got %d", len(list))
	}
	s.End("id1", "agent1")
	if _, ok := s.Live("agent1"); ok {
		t.Fatal("End must unregister the request from the owner index")
	}
	if _, ok := s.Get("id1"); ok {
		t.Fatal("End must unregister the request from the id index")
	}
	if !s.Begin("agent1", &struct{}{}, "id2") {
		t.Fatal("Begin must succeed again after End")
	}
}

func TestInteractionStoreAgentsAreIndependent(t *testing.T) {
	var s InteractionStore[int]
	if !s.Begin("a", 1, "x") || !s.Begin("b", 2, "y") {
		t.Fatal("different agents park freely")
	}
	if got, _ := s.Live("a"); got != 1 {
		t.Fatalf("a should see 1, got %d", got)
	}
	if got, _ := s.Live("b"); got != 2 {
		t.Fatalf("b should see 2, got %d", got)
	}
	s.End("x", "a")
	if _, ok := s.Live("b"); !ok {
		t.Fatal("ending one agent's request must not touch the other's")
	}
}
