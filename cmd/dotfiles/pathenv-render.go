package main

import (
	"fmt"
	"strings"
)

// renderPathenvBash returns the bash code of p: the stacking warning, a
// comment with the configuration and event, each entry after a comment
// saying where it is from, and PATHENV_ACTIVE last, so that the
// configuration counts as active whatever the entries do.
func renderPathenvBash(p pathenvPlan) (string, error) {
	var b strings.Builder
	if len(p.onTopOf) > 0 {
		msg, err := shellQuote(fmt.Sprintf("pathenv: %s activated on top of %s", p.name, strings.Join(p.onTopOf, " ")))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "echo %s >&2\n", msg)
	}
	fmt.Fprintf(&b, "# %s %s\n", p.name, p.event)
	for _, e := range p.entries {
		fmt.Fprintf(&b, "# %s\n", e.origin)
		b.WriteString(e.code)
		if !strings.HasSuffix(e.code, "\n") {
			b.WriteString("\n")
		}
	}
	if p.active != nil {
		active, err := shellQuote(strings.Join(p.active, " "))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "export PATHENV_ACTIVE=%s\n", active)
	}
	return b.String(), nil
}
