package main

import "testing"

func TestRedactSourceUserInfo(t *testing.T) {
	got := redactSourceUserInfo("https://user@example.com/org/repo.git")
	if got != "https://example.com/org/repo.git" {
		t.Fatalf("redactSourceUserInfo() = %q", got)
	}
}

func TestRedactSourceUserInfoLeavesNonURL(t *testing.T) {
	got := redactSourceUserInfo("owner/repo")
	if got != "owner/repo" {
		t.Fatalf("redactSourceUserInfo(owner/repo) = %q", got)
	}
}
