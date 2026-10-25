package main

import (
	"flag"
	"strings"
	"testing"
	"text/template"

	"github.com/peterbourgon/ff/v3"
)

func TestDocAppendices(t *testing.T) {
	t.Parallel()
	text := docAppendices()
	for line := range strings.Lines(text) {
		line = strings.TrimSuffix(line, "\n")
		if len(line) > 79 {
			t.Errorf("line longer than 79 columns: %q", line)
		}
		if strings.HasPrefix(line, "#") && line != "## Appendices" && !strings.HasPrefix(line, "### Appendix ") && !strings.HasSuffix(line, " Names") {
			t.Errorf("line would be a heading: %q", line)
		}
	}
	for name := range alwaysIgnoredFiles {
		if !strings.Contains(text, "\n  "+name+"\n") {
			t.Errorf("always ignored %s is not listed", name)
		}
	}
	for _, want := range []string{
		"\npublic ",
		"\n[[starlark]]\n  src ",
		"\n  missing    skip (the default), create or error\n",
		"\n[repositories.remote.<name>]\n  kind ",
		"\n[vars]\n  <name> ",
		"\n  -dry-run, DOTFILES_DRY_RUN\n",
		"(default $HOME/src/dotfiles)",
		"\n#### Template Names\n",
		"\nFunctions from dotfiles:\n    env exists expr hex0x hexDigits isDir isFile lookPath parseBool rgb\n    shellQuote\n",
		"\nValues from dotfiles:\n    arch doc dst hostname os theme vars\n",
		"\nFunctions from dotfiles:\n    env exists hex0x hexDigits isDir isFile lookPath merge parseBool read rgb\n",
		" filter ",
		" urlquery\n",
		" setdefault ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("appendices do not contain %q", want)
		}
	}

	// The appendices are the last top-level section, after the README's.
	toc, err := docTopic(docText(), "toc")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(toc, "\n"), "\n")
	last := lines[max(0, len(lines)-8):]
	n, _, _ := strings.Cut(last[0], " ")
	want := []string{
		n + " Appendices",
		"  " + n + ".1 Appendix A: Always Ignored Files",
		"  " + n + ".2 Appendix B: Configuration Keys",
		"  " + n + ".3 Appendix C: Options and Environment Variables",
		"  " + n + ".4 Appendix D: Built-in Names",
		"    " + n + ".4.1 Expression Names",
		"    " + n + ".4.2 Template Names",
		"    " + n + ".4.3 Starlark Names",
	}
	if strings.Join(last, "\n") != strings.Join(want, "\n") {
		t.Errorf("table of contents ends in\n%s\nwant\n%s", strings.Join(last, "\n"), strings.Join(want, "\n"))
	}
}

// TestTemplateBuiltins checks that the functions templateBuiltins lists are
// defined in templates, so that a removed one is noticed. One that a later
// version of Go adds is not.
func TestTemplateBuiltins(t *testing.T) {
	t.Parallel()
	for _, name := range templateBuiltins {
		if _, err := template.New("").Parse("{{" + name + "}}"); err != nil {
			t.Error(err)
		}
	}
	if _, err := template.New("").Parse("{{nosuch}}"); err == nil {
		t.Error("an undefined function parses; the test checks nothing")
	}
}

// TestEnvVarName checks that envVarName names the variables that ff reads.
func TestEnvVarName(t *testing.T) {
	var d Dotfiles
	fs := d.flagSet(t.TempDir())
	fs.VisitAll(func(f *flag.Flag) {
		value := "x"
		if f.DefValue == "false" {
			value = "true"
		}
		t.Setenv(envVarName(f.Name), value)
	})
	if err := ff.Parse(fs, nil, ff.WithEnvVarPrefix(envVarPrefix)); err != nil {
		t.Fatal(err)
	}
	fs.VisitAll(func(f *flag.Flag) {
		if f.Value.String() == f.DefValue {
			t.Errorf("-%s is not set by %s", f.Name, envVarName(f.Name))
		}
	})
}
