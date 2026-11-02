package main

import "slices"

// A pathenvPlan is what pathenv does in a shell: run the entries of one
// event of a configuration, and update PATHENV_ACTIVE.
type pathenvPlan struct {
	name    string         // the configuration
	event   string         // pathenvActivate or pathenvRefresh
	entries []pathenvEntry // those that run on event
	onTopOf []string       // the active configurations it is activated on top of, for the warning
	active  []string       // the new PATHENV_ACTIVE, or nil if it stays as it is
}

// planPathenv returns the plan for the configuration name with entries, in a
// shell where the configurations active are active, in activation order. A
// configuration that is active is refreshed, unless reload is set; one that
// is not is activated and appended to the list.
func planPathenv(name string, entries []pathenvEntry, active []string, reload bool) pathenvPlan {
	p := pathenvPlan{name: name, event: pathenvActivate}
	switch {
	case slices.Contains(active, name) && !reload:
		p.event = pathenvRefresh
	case slices.Contains(active, name):
		p.active = slices.Clone(active)
	default:
		p.onTopOf = slices.Clone(active)
		p.active = append(slices.Clone(active), name)
	}
	for _, e := range entries {
		if e.runsOn(p.event) {
			p.entries = append(p.entries, e)
		}
	}
	return p
}
