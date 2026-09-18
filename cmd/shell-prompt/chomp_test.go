package main

import (
	"testing"
)

func TestDirChomp(t *testing.T) {
	home := "/home/t"

	tests := []struct {
		name     string
		path     string
		maxLen   int
		expected string
	}{
		{
			name:     "home root",
			path:     "/home/t",
			maxLen:   10,
			expected: "~",
		},
		{
			name:     "home root short limit",
			path:     "/home/t",
			maxLen:   1,
			expected: "~",
		},
		{
			name:     "root dir",
			path:     "/",
			maxLen:   10,
			expected: "/",
		},
		{
			name:     "simple path within limit",
			path:     "/home/t/src/dotfiles/base",
			maxLen:   30,
			expected: "~/src/dotfiles/base",
		},
		{
			name:     "chomped home path len 5",
			path:     "/home/t/src/dotfiles/base",
			maxLen:   5,
			expected: "~/s/d/base",
		},
		{
			name:     "chomped home path len 10",
			path:     "/home/t/src/dotfiles/base",
			maxLen:   10,
			expected: "~/s/d/base",
		},
		{
			name:     "chomped home path len 15",
			path:     "/home/t/src/dotfiles/base",
			maxLen:   15,
			expected: "~/s/d/base",
		},
		{
			name:     "chomped home path len 20",
			path:     "/home/t/src/dotfiles/base",
			maxLen:   20,
			expected: "~/src/dotfiles/base",
		},
		{
			name:     "dotfile path chomp",
			path:     "/home/t/.config/sub/deep/dir",
			maxLen:   15,
			expected: "~/.c/s/deep/dir",
		},
		{
			name:     "dotfile path chomp 20",
			path:     "/home/t/.config/fish/functions",
			maxLen:   20,
			expected: "~/.c/fish/functions",
		},
		{
			name:     "non-home system path chomped",
			path:     "/var/log/nginx/access.log",
			maxLen:   12,
			expected: "/v/l/n/access.log",
		},
		{
			name:     "tmp subpath",
			path:     "/tmp/one",
			maxLen:   5,
			expected: "/t/one",
		},
		{
			name:     "brackets and multiple spaces",
			path:     "/home/t/a [test]  dir/foo",
			maxLen:   20,
			expected: "~/a test] dir/foo",
		},
		{
			name:     "already short path no chomp",
			path:     "/tmp",
			maxLen:   10,
			expected: "/tmp",
		},
		{
			name:     "deep directory that needs full chomp",
			path:     "/a/b/c/d/e/f",
			maxLen:   10,
			expected: "/a/b/c/d/e/f",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DirChomp(tc.path, tc.maxLen, home)
			if got != tc.expected {
				t.Errorf("DirChomp(%q, %d) = %q, want %q", tc.path, tc.maxLen, got, tc.expected)
			}
		})
	}
}
