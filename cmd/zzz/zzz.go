package main

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const CleanupInterval = 100 // remove dead entries every nth call to add.

// Flags holds the CLI configuration
type Flags struct {
	Add     string
	Remove  string
	List    bool
	Cleanup bool
	Debug   bool
}

func (f *Flags) Register(fs *flag.FlagSet) {
	fs.StringVar(&f.Add, "add", "", "Add a new path to the database")
	fs.StringVar(&f.Remove, "remove", "", "remove a path from the database")
	fs.BoolVar(&f.List, "l", false, "List matches with their scores")
	fs.BoolVar(&f.Cleanup, "cleanup", false, "Remove entries that are no longer valid directories")
	fs.BoolVar(&f.Debug, "debug", false, "print debug info")
}

type Entry struct {
	Path  string
	Rank  float64
	Time  int64
	Score float64
}

type Store struct {
	Path   string
	Stderr io.Writer
}

func (s *Store) LoadCounter() (int, error) {
	data, err := os.ReadFile(s.Path + ".count")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) SaveCounter(count int) error {
	return os.WriteFile(s.Path+".count", []byte(strconv.Itoa(count)), 0644)
}

func (s *Store) LoadEntries() ([]Entry, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}

	var results []Entry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		rank, _ := strconv.ParseFloat(parts[1], 64)
		t, _ := strconv.ParseInt(parts[2], 10, 64)
		results = append(results, Entry{Path: parts[0], Rank: rank, Time: t})
	}
	return results, nil

}

func (s *Store) SaveEntries(entries []Entry) error {
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("%s|%v|%d\n", e.Path, e.Rank, e.Time))
	}
	return os.WriteFile(s.Path, []byte(sb.String()), 0644)
}

func frecent(rank float64, lastTime int64) float64 {
	dx := time.Now().Unix() - lastTime
	return 10000 * rank * (3.75 / (0.0001*float64(dx) + 1.25))
}

func main() {
	datafile := os.Getenv("_Z_DATA")
	if datafile == "" {
		datafile = filepath.Join(os.Getenv("HOME"), ".z")
	}

	f := &Flags{}
	f.Register(flag.CommandLine)
	flag.Parse()

	stderr := io.Discard
	if f.Debug || os.Getenv("ZZZ_DEBUG") == "1" {
		stderr = os.Stderr
	}

	store := &Store{Path: datafile, Stderr: stderr}

	if f.Cleanup {
		cleanupEntries(store)
		return
	}

	if f.Add != "" {
		addEntry(store, f.Add)
		return
	}
	if f.Remove != "" {
		removeEntry(store, f.Remove)
		return
	}

	queryArgs := flag.Args()
	if len(queryArgs) > 0 || f.List {
		runSearch(store, queryArgs, f.List)
	}
}

func cleanupEntries(s *Store) {
	prevEntries, err := s.LoadEntries()
	if err != nil {
		fmt.Fprintf(s.Stderr, "[zzz] error reading data file=%s\n", s.Path)
		return
	}

	var nextEntries []Entry
	for _, entry := range prevEntries {
		info, err := os.Stat(entry.Path)
		if err == nil && info.IsDir() {
			nextEntries = append(nextEntries, entry)
		} else {
			fmt.Fprintf(s.Stderr, "[zzz] removing path=%s\n", entry.Path)
		}
	}

	if err := s.SaveEntries(nextEntries); err != nil {
		fmt.Fprintf(s.Stderr, "error saving database: %v\n", err)
		return
	}

}

func addEntry(s *Store, newPath string) {
	if newPath == os.Getenv("HOME") || newPath == "/" {
		return
	}
	prevEntries, err := s.LoadEntries()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(s.Stderr, "[zzz] error reading data file path=%s\n", s.Path)
	}

	var found bool
	var totalRank float64
	var nextEntries []Entry

	for _, entry := range prevEntries {
		if entry.Path == newPath {
			entry.Rank++
			fmt.Fprintf(s.Stderr, "[zzz] increase rank=%.2f path=%s\n", entry.Rank, newPath)
			entry.Time = time.Now().Unix()
			found = true
		}
		totalRank += entry.Rank
		nextEntries = append(nextEntries, entry)
	}

	// TODO: this probalby should change
	if totalRank > 9000 {
		for i := range nextEntries {
			nextEntries[i].Rank *= 0.99
		}
	}

	if !found {
		fmt.Fprintf(s.Stderr, "[zzz] add path=%s\n", newPath)
		nextEntries = append(nextEntries, Entry{Path: newPath, Rank: 1, Time: time.Now().Unix()})
	}

	if err := s.SaveEntries(nextEntries); err != nil {
		fmt.Fprintf(s.Stderr, "error saving database: %v\n", err)
		return
	}

	checkAndTriggerCleanup(s)
}

func removeEntry(s *Store, removePath string) {
	if removePath == os.Getenv("HOME") || removePath == "/" {
		return
	}
	entries, err := s.LoadEntries()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(s.Stderr, "[zzz] error reading data file path=%s\n", s.Path)
	}
	entries = slices.DeleteFunc(entries, func(entry Entry) bool {
		return entry.Path == removePath
	})
	if err := s.SaveEntries(entries); err != nil {
		fmt.Fprintf(s.Stderr, "error saving database: %v\n", err)
		return
	}
	checkAndTriggerCleanup(s)
}

func checkAndTriggerCleanup(s *Store) {
	count, err := s.LoadCounter()
	if err != nil {
		fmt.Fprintf(s.Stderr, "failed to load counter err=%v\n", err)
	}

	count++

	if count >= CleanupInterval {
		if err := s.SaveCounter(0); err != nil {
			fmt.Fprintf(s.Stderr, "failed to save counter err=%v\n", err)
		}

		exe, err := os.Executable()
		if err != nil {
			exe = os.Args[0] // should not happen
		}

		cmd := exec.Command(exe, "-cleanup")
		cmd.Stdin = nil
		cmd.Stdout = nil
		cmd.Stderr = nil

		if err := cmd.Start(); err != nil {
			fmt.Fprintf(s.Stderr, "failed to start cleanup err=%v\n", err)
		}
	} else {
		if err := s.SaveCounter(count); err != nil {
			fmt.Fprintf(s.Stderr, "failed to save counter err=%v\n", err)
		}
	}
}

func runSearch(s *Store, queryParts []string, listMode bool) {
	var matches []Entry
	pattern := strings.Join(queryParts, ".*")
	reg := regexp.MustCompile("(?i)" + pattern)

	entries, err := s.LoadEntries()
	if err != nil {
		fmt.Fprintf(s.Stderr, "[zzz] error reading data file=%s\n ", s.Path)
		return
	}

	for _, entry := range entries {
		if reg.MatchString(entry.Path) {
			entry.Score = frecent(entry.Rank, entry.Time)
			matches = append(matches, entry)
		}
	}

	slices.SortFunc(matches, func(a, b Entry) int {
		return cmp.Compare(a.Score, b.Score)
	})

	if listMode {
		for _, m := range matches {
			fmt.Printf("%-10.0f %s\n", m.Score, m.Path)
		}
	} else if len(matches) > 0 {
		fmt.Print(matches[len(matches)-1].Path)
	}
}
