package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

type fieldKind int

const (
	kindPlain fieldKind = iota
	kindSecret
	kindOTP
)

// menuField is a field offered in the field menu.
type menuField struct {
	Label string
	Kind  fieldKind
	Value string
}

// line is the menu line of f, with secrets masked.
func (f menuField) line() string {
	shown := masked
	if f.Kind == kindPlain {
		shown = oneLine(f.Value)
	}
	return fmt.Sprintf("%-24s %s", f.Label, shown)
}

const masked = "••••••••"

// menuFields lists the fields of item that have a value: username,
// password, one-time password, the others in item order, then URLs.
func menuFields(item Item) []menuField {
	sorted := slices.Clone(item.Fields)
	slices.SortStableFunc(sorted, func(a, b Field) int { return cmp.Compare(fieldRank(a), fieldRank(b)) })
	var fields []menuField
	for _, f := range sorted {
		mf := menuField{Label: fieldLabel(f), Value: f.Value}
		switch f.Type {
		case "OTP":
			mf.Kind, mf.Value = kindOTP, f.TOTP
		case "CONCEALED", "SSHKEY":
			mf.Kind = kindSecret
		}
		if mf.Value != "" {
			fields = append(fields, mf)
		}
	}
	for _, u := range item.URLs {
		if u.Href != "" {
			fields = append(fields, menuField{Label: oneLine(cmp.Or(u.Label, "url")), Value: u.Href})
		}
	}
	return fields
}

func fieldRank(f Field) int {
	switch {
	case f.Purpose == "USERNAME":
		return 0
	case f.Purpose == "PASSWORD":
		return 1
	case f.Type == "OTP":
		return 2
	}
	return 3
}

func fieldLabel(f Field) string {
	label := cmp.Or(f.Label, f.ID)
	if f.Purpose == "NOTES" {
		label = "notes"
	}
	if f.Section != nil && f.Section.Label != "" {
		label = f.Section.Label + " › " + label
	}
	return oneLine(label)
}

// oneLine is the first line of s, shortened, for a menu.
func oneLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	s = strings.ReplaceAll(s, "\t", " ")
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60]) + "…"
	}
	return s
}
