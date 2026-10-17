package main

import (
	_ "embed"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DocCommand prints the README and its appendices, or with arguments, the
// sections of them that match the arguments as a topic (see docTopic).
func (d *Dotfiles) DocCommand(args []string) error {
	text := docText()
	if len(args) == 0 {
		fmt.Fprint(d.Stdout, text)
		return nil
	}
	out, err := docTopic(text, strings.Join(args, " "))
	if err != nil {
		fmt.Fprintln(d.Stderr, err)
		return err
	}
	fmt.Fprint(d.Stdout, out)
	return nil
}

//go:generate go run ./internal/cmd/gendoc README.md README.txt

// readme is README.md rendered as plain text by glow. Headings keep their
// leading #s and code blocks are indented, so docSections still finds the
// sections.
//
//go:embed README.txt
var readme string

// docSection is a section of a Markdown document: a heading and the lines
// after it, up to the next heading of the same or a higher level.
type docSection struct {
	level      int
	title      string
	number     string // "2.1"; empty for the document title
	depth      int    // number of numbered sections this one is inside
	start, end int    // line indexes; end is exclusive
}

// docSections returns the sections of the Markdown lines, in order. Lines
// inside fenced code blocks are never headings.
func docSections(lines []string) []docSection {
	var sections []docSection
	var fence string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}
		level := len(line) - len(strings.TrimLeft(line, "#"))
		if level == 0 || level > 6 || !strings.HasPrefix(line[level:], " ") {
			continue
		}
		sections = append(sections, docSection{level: level, title: strings.TrimSpace(line[level:]), start: i})
	}
	for i := range sections {
		sections[i].end = len(lines)
		for _, next := range sections[i+1:] {
			if next.level <= sections[i].level {
				sections[i].end = next.start
				break
			}
		}
	}
	numberSections(sections)
	return sections
}

// numberSections numbers sections like 1, 1.1, 1.2, 2. A first heading that is
// the only one at its level is the document's title and gets no number.
func numberSections(sections []docSection) {
	numbered := sections
	if len(sections) > 0 {
		title := true
		for _, s := range sections[1:] {
			if s.level <= sections[0].level {
				title = false
				break
			}
		}
		if title {
			numbered = sections[1:]
		}
	}
	var levels []int // headings levels of the sections the current one is inside
	var counts []int // section counts at each depth
	for i := range numbered {
		s := &numbered[i]
		for len(levels) > 0 && levels[len(levels)-1] >= s.level {
			levels = levels[:len(levels)-1]
		}
		s.depth = len(levels)
		counts = counts[:min(len(counts), s.depth+1)]
		if len(counts) > s.depth {
			counts[s.depth]++
		} else {
			counts = append(counts, 1)
		}
		parts := make([]string, len(counts))
		for j, c := range counts {
			parts[j] = strconv.Itoa(c)
		}
		s.number = strings.Join(parts, ".")
		levels = append(levels, s.level)
	}
}

// docTOC returns the table of contents of sections: each numbered heading on
// its own line, indented by depth, without Markdown backquotes.
func docTOC(sections []docSection) string {
	var b strings.Builder
	for _, s := range sections {
		title := strings.ReplaceAll(s.title, "`", "")
		if s.number == "" {
			fmt.Fprintf(&b, "%s\n", title)
			continue
		}
		fmt.Fprintf(&b, "%s%s %s\n", strings.Repeat("  ", s.depth), s.number, title)
	}
	return b.String()
}

// docNumberRe matches a section number, such as 2.1 or 2.1.
var docNumberRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*\.?$`)

// normalizeTopic returns s in lower case, without backquotes and square
// brackets and with runs of white space as one space, for comparing topics
// with headings, so that "mount" is the heading `[[mount]]`.
func normalizeTopic(s string) string {
	s = strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}

// docTopic returns part of the Markdown text: for the topic "toc", its
// numbered table of contents; for a section number such as 2.1, that
// section; otherwise the sections whose heading is topic, ignoring case,
// backquotes and square brackets, or, if none is, the sections whose heading contains topic. A
// section inside one already returned is left out. If nothing matches, the
// error shows the table of contents.
func docTopic(text, topic string) (string, error) {
	lines := strings.Split(text, "\n")
	sections := docSections(lines)
	topic = normalizeTopic(topic)
	if topic == "toc" {
		return docTOC(sections), nil
	}

	var matches []docSection
	if docNumberRe.MatchString(topic) {
		for _, s := range sections {
			if s.number == strings.TrimSuffix(topic, ".") {
				matches = append(matches, s)
			}
		}
	}
	for _, exact := range []bool{true, false} {
		if len(matches) > 0 {
			break
		}
		for _, s := range sections {
			title := normalizeTopic(s.title)
			if exact && title == topic || !exact && strings.Contains(title, topic) {
				matches = append(matches, s)
			}
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no section matches %q; the sections are:\n\n%s", topic, strings.TrimSuffix(docTOC(sections), "\n"))
	}

	var b strings.Builder
	end := -1
	for _, s := range matches {
		if s.start < end {
			continue // inside the previous match
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimRight(strings.Join(lines[s.start:s.end], "\n"), "\n"))
		b.WriteString("\n")
		end = s.end
	}
	return b.String(), nil
}
