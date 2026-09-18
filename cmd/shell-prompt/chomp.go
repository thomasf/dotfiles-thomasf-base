package main

import (
	"strings"
	"unicode/utf8"
)

// DirChomp shortens a directory path so its length is within maxLen if possible,
// matching the algorithm used in bash __dir_chomp.
func DirChomp(path string, maxLen int, homeDir string) string {
	p := path
	if homeDir != "" {
		if p == homeDir {
			p = "~"
		} else if strings.HasPrefix(p, homeDir+"/") {
			p = "~" + p[len(homeDir):]
		} else if strings.HasPrefix(p, homeDir) {
			p = "~" + p[len(homeDir):]
		}
	}

	// Remove '[' from strings
	p = strings.ReplaceAll(p, "[", " ")
	// Collapse double spaces
	p = strings.ReplaceAll(p, "  ", " ")

	s := len(p)
	b := ""

	// While p contains '/' and current length s > maxLen
	for strings.Contains(p, "/") && s > maxLen {
		if strings.HasPrefix(p, "/") {
			p = p[1:]
		}

		match := matchDotRune(p)
		b = b + "/" + match

		// Strip up to the first '/'
		if idx := strings.Index(p, "/"); idx >= 0 {
			p = p[idx+1:]
		} else {
			p = ""
		}
		s = len(b) + len(p)
	}

	// Replicate bash: ${b/\/~/\~}${b+/}$p
	if strings.HasPrefix(b, "/~") {
		b = "~" + b[2:]
	}
	if b != "" {
		b = b + "/"
	}
	return b + p
}

// matchDotRune replicates bash regex [[ $p =~ \.?. ]]
// If string starts with '.', it matches '.' plus the next rune (if available).
// Otherwise, it matches the first rune.
func matchDotRune(s string) string {
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	if r == '.' {
		rem := s[size:]
		if rem != "" {
			_, nextSize := utf8.DecodeRuneInString(rem)
			return s[:size+nextSize]
		}
		return s[:size]
	}
	return s[:size]
}
