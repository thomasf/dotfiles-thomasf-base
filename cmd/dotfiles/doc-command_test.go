package main

import (
	"bytes"
	"strings"
	"testing"
)

const docTestText = `# Tool

Intro.

## Install

Run it.

` + "```toml" + `
# not a heading
` + "```" + `

## ` + "`[[starlark]]`" + ` Entries

Starlark.

### Starlark Values

Values.

## Install Order

Order.
`

func TestDocTopic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		topic   string
		want    []string // headings of the printed sections, in order
		wantErr bool
	}{
		// An exact match wins over sections that only contain the topic.
		{topic: "install", want: []string{"## Install"}},
		{topic: "INSTALL ORDER", want: []string{"## Install Order"}},
		// Backquotes and brackets are ignored; a subsection inside a match is
		// not repeated.
		{topic: "[[starlark]] entries", want: []string{"## `[[starlark]]` Entries", "### Starlark Values"}},
		{topic: "starlark entries", want: []string{"## `[[starlark]]` Entries", "### Starlark Values"}},
		{topic: "starlark", want: []string{"## `[[starlark]]` Entries", "### Starlark Values"}},
		{topic: "values", want: []string{"### Starlark Values"}},
		// Sections by number; the title is not numbered.
		{topic: "2", want: []string{"## `[[starlark]]` Entries", "### Starlark Values"}},
		{topic: "2.1.", want: []string{"### Starlark Values"}},
		{topic: "3", want: []string{"## Install Order"}},
		{topic: "4", wantErr: true},
		{topic: "not a heading", wantErr: true},
		{topic: "nope", wantErr: true},
	}
	for _, tt := range tests {
		got, err := docTopic(docTestText, tt.topic)
		if tt.wantErr {
			if err == nil {
				t.Errorf("%q: expected an error, got\n%s", tt.topic, got)
			} else if !strings.Contains(err.Error(), "\n3 Install Order") {
				t.Errorf("%q: error does not list the sections: %v", tt.topic, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tt.topic, err)
			continue
		}
		var headings []string
		for line := range strings.SplitSeq(got, "\n") {
			if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "# not") {
				headings = append(headings, line)
			}
		}
		if strings.Join(headings, "|") != strings.Join(tt.want, "|") {
			t.Errorf("%q: got sections %q, want %q", tt.topic, headings, tt.want)
		}
	}

	// A section ends at the next heading of the same or a higher level.
	got, _ := docTopic(docTestText, "install")
	if want := "## Install\n\nRun it.\n\n```toml\n# not a heading\n```\n"; got != want {
		t.Errorf("install: got %q, want %q", got, want)
	}
}

func TestDocCommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if err := new(Dotfiles).Run(&stdout, &stderr, []string{"doc", "starlark"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if !strings.HasPrefix(out, "### [[starlark]]\n") || !strings.Contains(out, "#### Running Scripts Again") || strings.Contains(out, "### Conditions") {
		t.Errorf("doc starlark printed the wrong sections:\n%s", out)
	}

	stdout.Reset()
	if err := new(Dotfiles).Run(&stdout, &stderr, []string{"doc"}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != docText() {
		t.Error("doc without a topic does not print the README and the appendices")
	}
}

func TestDocTOC(t *testing.T) {
	t.Parallel()
	got, err := docTopic(docTestText, "toc")
	if err != nil {
		t.Fatal(err)
	}
	want := `Tool
1 Install
2 [[starlark]] Entries
  2.1 Starlark Values
3 Install Order
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	// Without a single title, every heading is numbered, and skipped
	// heading levels do not leave gaps in the numbers.
	got, _ = docTopic("# A\n#### A deep\n# B\n", "toc")
	if want := "1 A\n  1.1 A deep\n2 B\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
