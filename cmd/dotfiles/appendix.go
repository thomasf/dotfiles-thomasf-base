package main

import (
	"flag"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/expr-lang/expr/builtin"
	"go.starlark.net/starlark"
)

// docText returns the text that the doc command prints: the README followed
// by the appendices, which are generated from the program so that they always
// match it.
func docText() string {
	return readme + "\n" + docAppendices()
}

// docAppendices returns the appendices of the manual, as plain text like
// README.txt: a top-level section Appendices with one section per appendix.
func docAppendices() string {
	appendices := []struct {
		title string
		text  func() string
	}{
		{"Always Ignored Files", ignoredFilesAppendix},
		{"Configuration Keys", configKeysAppendix},
		{"Options and Environment Variables", optionsAppendix},
		{"Built-in Names", builtinsAppendix},
	}
	var b strings.Builder
	b.WriteString("## Appendices\n")
	for i, a := range appendices {
		fmt.Fprintf(&b, "\n### Appendix %c: %s\n\n%s", 'A'+i, a.title, a.text())
	}
	return b.String()
}

func ignoredFilesAppendix() string {
	var b strings.Builder
	b.WriteString(wrap("These files and directories are never linked or copied, neither as root "+
		"items nor as the source of a [[mount]] entry or a file its pattern matches:", 0))
	b.WriteString("\n")
	for _, name := range slices.Sorted(maps.Keys(alwaysIgnoredFiles)) {
		fmt.Fprintf(&b, "  %s\n", name)
	}
	b.WriteString("\nNor are files whose names match these patterns:\n\n")
	for _, pattern := range alwaysIgnoredPatterns {
		fmt.Fprintf(&b, "  %s\n", pattern)
	}
	b.WriteString("\n[[ignore]] entries add more patterns.\n")
	return b.String()
}

// configKeyValues describes the values of the dotfiles.toml keys that take
// one of a few strings, or a particular form of one, by their path. <name>
// stands for the key of a table.
var configKeyValues = map[string]string{
	"script.phase":                    "pre, post or default (the default)",
	"template.mode":                   `octal permissions, such as "0644"`,
	"template.existing":               fmt.Sprintf("%s (the default), %s or %s", ExistingOverwrite, ExistingSkip, ExistingError),
	"starlark.missing":                fmt.Sprintf("%s (the default), %s or %s", MissingSkip, MissingCreate, MissingError),
	"starlark.format":                 fmt.Sprintf("%s or %s (by default from the extension)", formatJSON, formatTOML),
	"starlark.indent":                 fmt.Sprintf("integer (default %d)", defaultIndent),
	"starlark.mode":                   `octal permissions, such as "0644"`,
	"repositories.remote.<name>.kind": fmt.Sprintf("%s, %s or %s", RemoteKindPrivate, RemoteKindBackup, RemoteKindPublish),
}

func configKeysAppendix() string {
	var b strings.Builder
	b.WriteString(wrap("The keys of dotfiles.toml by section; any other key is an error. "+
		"dotfiles doc <section>, e.g. dotfiles doc mount, describes a section.", 0))
	sections := configSections(reflect.TypeFor[Config](), nil, "")
	width := 0
	for _, s := range sections {
		for _, k := range s.keys {
			width = max(width, len(k.name))
		}
	}
	for _, s := range sections {
		b.WriteString("\n")
		indent := ""
		if s.header != "" {
			fmt.Fprintf(&b, "%s\n", s.header)
			indent = "  "
		}
		for _, k := range s.keys {
			fmt.Fprintf(&b, "%s%-*s  %s\n", indent, width+2-len(indent), k.name, k.value)
		}
	}
	return b.String()
}

// A configSection is a table of dotfiles.toml: its header, such as
// [[mount]], or "" for the top level, and its keys.
type configSection struct {
	header string
	keys   []configKey
}

type configKey struct {
	name  string
	value string // what the value is: a TOML type or the values it can take
}

// configSections returns the section of the TOML table described by the
// struct type t, at path and with header, followed by the sections inside
// it, in the order of the fields of t. A section without keys is left out.
func configSections(t reflect.Type, path []string, header string) []configSection {
	s := configSection{header: header}
	var inner []configSection
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
		p := append(slices.Clip(path), name)
		key := strings.Join(p, ".")
		ft := f.Type
		switch {
		case ft.Kind() == reflect.Struct:
			inner = append(inner, configSections(ft, p, "["+key+"]")...)
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct:
			inner = append(inner, configSections(ft.Elem(), p, "[["+key+"]]")...)
		case ft.Kind() == reflect.Map && ft.Elem().Kind() == reflect.Struct:
			inner = append(inner, configSections(ft.Elem(), append(p, "<name>"), "["+key+".<name>]")...)
		case ft.Kind() == reflect.Map:
			inner = append(inner, configSection{
				header: "[" + key + "]",
				keys:   []configKey{{"<name>", tomlType(ft.Elem())}},
			})
		default:
			s.keys = append(s.keys, configKey{name, configKeyValue(key, ft)})
		}
	}
	if len(s.keys) == 0 {
		return inner
	}
	return append([]configSection{s}, inner...)
}

// configKeyValue describes the value of the key at path, of type t.
func configKeyValue(path string, t reflect.Type) string {
	if v, ok := configKeyValues[path]; ok {
		return v
	}
	if strings.HasSuffix(path, ".condition") {
		return "expression"
	}
	return tomlType(t)
}

// tomlType returns the name of the TOML type that values of t are decoded
// from.
func tomlType(t reflect.Type) string {
	switch {
	case t == reflect.TypeFor[StringList]():
		return "string or list of strings"
	case t.Kind() == reflect.String:
		return "string"
	case t.Kind() == reflect.Bool:
		return "boolean"
	case t.Kind() == reflect.Int:
		return "integer"
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.String:
		return "list of strings"
	}
	panic("no TOML type for " + t.String())
}

func optionsAppendix() string {
	var b strings.Builder
	b.WriteString(wrap("The options go before the command, e.g. dotfiles -v install. Each can "+
		"also be set with its environment variable, e.g. DOTFILES_V=true; an option "+
		"on the command line wins over its environment variable.", 0))
	b.WriteString("\n")
	new(Dotfiles).flagSet("$HOME").VisitAll(func(f *flag.Flag) {
		arg, usage := flag.UnquoteUsage(f)
		fmt.Fprintf(&b, "  -%s", f.Name)
		if arg != "" {
			fmt.Fprintf(&b, " %s", arg)
		}
		fmt.Fprintf(&b, ", %s\n", envVarName(f.Name))
		if f.DefValue != "" && f.DefValue != "false" {
			usage += fmt.Sprintf(" (default %s)", f.DefValue)
		}
		b.WriteString(wrap(usage, 6))
	})
	return b.String()
}

// wrap returns the words of s as lines of at most 79 columns, each indented
// by indent spaces and ending in a newline.
func wrap(s string, indent int) string {
	var b strings.Builder
	col := 0
	for _, word := range strings.Fields(s) {
		switch {
		case col == 0:
		case col+1+len(word) > 79:
			b.WriteString("\n")
			col = 0
		default:
			b.WriteString(" ")
			col++
		}
		if col == 0 {
			b.WriteString(strings.Repeat(" ", indent))
			col = indent
		}
		b.WriteString(word)
		col += len(word)
	}
	b.WriteString("\n")
	return b.String()
}

func builtinsAppendix() string {
	var b strings.Builder
	b.WriteString(wrap("The variables and functions that expressions, templates and Starlark "+
		"scripts can use: those that dotfiles provides, then those of expr, Go "+
		"templates and Starlark.", 0))
	section := func(title, intro string) {
		fmt.Fprintf(&b, "\n#### %s\n\n", title)
		b.WriteString(wrap(intro, 0))
	}
	list := func(title string, names []string) {
		fmt.Fprintf(&b, "\n%s:\n", title)
		b.WriteString(wrap(strings.Join(names, " "), 4))
	}

	vars := slices.Sorted(maps.Keys(exprVars(evalContext{})))
	exprBuiltins := make([]string, len(builtin.Builtins))
	for i, f := range builtin.Builtins {
		exprBuiltins[i] = f.Name
	}
	section("Expression Names", "In conditions and {{expr \"...\"}}. The sections Expressions and "+
		"Conditions and Useful expr Features describe the ones that come up most.")
	list("Variables from dotfiles", vars)
	list("Functions from dotfiles", slices.Sorted(maps.Keys(exprFuncs(""))))
	list("Functions from expr", slices.Sorted(slices.Values(exprBuiltins)))

	fields := make([]string, len(vars))
	for i, name := range vars {
		fields[i] = "." + name
	}
	section("Template Names", "In [vars] values, templated paths, [git] values, [[script]] code and "+
		"[[template]] files. The section Templates describes the ones that come up most.")
	list("Fields from dotfiles", fields)
	list("Functions from dotfiles", slices.Sorted(maps.Keys(templateFuncs(evalContext{}))))
	list("Functions from Go", templateBuiltins)

	starlarkNames := starlarkPredeclared("", "", evalContext{}, starlark.NewDict(0))
	section("Starlark Names", "In [[starlark]] code. The sections Names in Scripts and Useful "+
		"Starlark Built-ins describe the ones that come up most.")
	list("Values from dotfiles", starlarkValues(starlarkNames, false))
	list("Functions from dotfiles", starlarkValues(starlarkNames, true))
	list("Values from Starlark", starlarkValues(starlark.Universe, false))
	list("Functions from Starlark", starlarkValues(starlark.Universe, true))
	list("dict methods", starlark.NewDict(0).AttrNames())
	list("list methods", starlark.NewList(nil).AttrNames())
	list("string methods", starlark.String("").AttrNames())
	return b.String()
}

// starlarkValues returns the sorted names in names whose values are
// functions, or with functions false, are not.
func starlarkValues(names starlark.StringDict, functions bool) []string {
	var selected []string
	for name, v := range names {
		if _, ok := v.(starlark.Callable); ok == functions {
			selected = append(selected, name)
		}
	}
	slices.Sort(selected)
	return selected
}
