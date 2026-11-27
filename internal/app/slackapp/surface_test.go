package slackapp

import "testing"

func TestStrictThread(t *testing.T) {
	if strictThread(true, false) {
		t.Fatal("a direct message with no quote asks on top of workspace policy")
	}
	if !strictThread(false, false) {
		t.Fatal("a channel turn does not ask")
	}
	if !strictThread(true, true) {
		t.Fatal("a direct message that quotes someone else does not ask")
	}
}
