package main

import "testing"

func TestBackgroundStatusDoesNotNeedPolling(t *testing.T) {
	if got := backgroundStatus(0); got != "Status: Ready" {
		t.Fatalf("idle status = %q", got)
	}
	if got := backgroundStatus(2); got != "Status: Working" {
		t.Fatalf("active status = %q", got)
	}
}
