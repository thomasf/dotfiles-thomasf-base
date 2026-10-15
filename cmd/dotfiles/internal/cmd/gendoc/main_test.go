package main

import "testing"

func TestTrim(t *testing.T) {
	t.Parallel()
	got := trim("\n  # Title   \n          \n  Text.  \n\n    code  \n  \n")
	if want := "# Title\n\nText.\n\n  code\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
