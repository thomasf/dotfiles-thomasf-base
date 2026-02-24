package main

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Entry struct {
	Path  string
	Rank  float64
	Time  int64
	Score float64 // Calculated frecency
}

func frecent(rank float64, lastTime int64) float64 {
	dx := time.Now().Unix() - lastTime
	// Formula from original z.sh: 10000 * rank * (3.75/((0.0001 * dx + 1) + 0.25))
	return 10000 * rank * (3.75 / (0.0001*float64(dx) + 1.25))
}

func main() {
	datafile := os.Getenv("_Z_DATA")
	if datafile == "" {
		datafile = filepath.Join(os.Getenv("HOME"), ".z")
	}

	if len(os.Args) < 2 {
		return
	}

	if os.Args[1] == "--add" && len(os.Args) > 2 {
		addEntry(datafile, os.Args[2])
		return
	}

	// TODO: add cleanup function to remove non directories

	runSearch(datafile, os.Args[1:])
}

func addEntry(datafile, newPath string) {
	if newPath == os.Getenv("HOME") || newPath == "/" {
		return
	}

	content, _ := os.ReadFile(datafile)
	lines := strings.Split(string(content), "\n")
	found := false
	var out []string

	totalRank := 0.0
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if parts[0] == newPath {
			rank, _ := strconv.ParseFloat(parts[1], 64)
			out = append(out, fmt.Sprintf("%s|%v|%d", parts[0], rank+1, time.Now().Unix()))
			totalRank += rank + 1
			found = true
		} else {
			out = append(out, line)
			r, _ := strconv.ParseFloat(parts[1], 64)
			totalRank += r
		}
	}

	if !found {
		out = append(out, fmt.Sprintf("%s|1|%d", newPath, time.Now().Unix()))
	}

	// Aging: if total rank > 9000, multiply all by 0.99
	if totalRank > 9000 {
		for i, line := range out {
			p := strings.Split(line, "|")
			r, _ := strconv.ParseFloat(p[1], 64)
			out[i] = fmt.Sprintf("%s|%v|%s", p[0], r*0.99, p[2])
		}
	}

	os.WriteFile(datafile, []byte(strings.Join(out, "\n")), 0644)
}

func runSearch(datafile string, args []string) {
	// Simple flag parsing
	listMode := false
	queryParts := []string{}
	for _, arg := range args {
		if arg == "-l" {
			listMode = true
		} else {
			queryParts = append(queryParts, arg)
		}
	}

	content, _ := os.ReadFile(datafile)
	lines := strings.Split(string(content), "\n")
	var matches []Entry

	pattern := strings.Join(queryParts, ".*")
	reg, _ := regexp.Compile("(?i)" + pattern)

	for _, line := range lines {
		if line == "" {
			continue
		}
		p := strings.Split(line, "|")
		if reg.MatchString(p[0]) {
			rank, _ := strconv.ParseFloat(p[1], 64)
			t, _ := strconv.ParseInt(p[2], 10, 64)
			matches = append(matches, Entry{p[0], rank, t, frecent(rank, t)})
		}
	}

	slices.SortFunc(matches, func(a, b Entry) int { return cmp.Compare(a.Score, b.Score) })

	if listMode {
		for _, m := range matches {
			fmt.Printf("%-10.0f %s\n", m.Score, m.Path)
		}
	} else if len(matches) > 0 {
		fmt.Print(matches[len(matches)-1].Path)
	}
}
