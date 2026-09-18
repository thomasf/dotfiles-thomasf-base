package main

import (
	"testing"
)

func TestFormatGitStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "clean master",
			input:    "## master...origin/master\n",
			expected: " (master)",
		},
		{
			name:     "modified unstaged",
			input:    "## master...origin/master\n M file.txt\n",
			expected: " (master *)",
		},
		{
			name:     "staged changes",
			input:    "## feature\nM  file.txt\n",
			expected: " (feature +)",
		},
		{
			name:     "untracked files",
			input:    "## main\n?? untracked.txt\n",
			expected: " (main %)",
		},
		{
			name:     "all flags",
			input:    "## main\nM  staged.txt\n M unstaged.txt\n?? new.txt\n",
			expected: " (main *+%)",
		},
		{
			name:     "detached HEAD",
			input:    "## HEAD (no branch)\n",
			expected: " (HEAD)",
		},
		{
			name:     "initial commit",
			input:    "## Initial commit on main\n",
			expected: " (main)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatGitStatus(tc.input)
			if got != tc.expected {
				t.Errorf("formatGitStatus(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestMinimalshellVCS(t *testing.T) {
	provider := &DefaultVCSProvider{}
	vcs := provider.GetVCS(".", ":minimalshell:")
	if vcs != "" {
		t.Errorf("expected empty VCS for :minimalshell:, got %q", vcs)
	}
}
