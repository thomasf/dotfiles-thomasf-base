// emacs-tree-sitter-installer is based on https://github.com/casouri/tree-sitter-module/releases
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type App struct {
	TargetABI   int
	MinABI      int
	CacheDir    string
	OutputDir   string
	BuildTmpDir string
	Verbose     bool
	Out         io.Writer
	Stdout      io.Writer
	Stderr      io.Writer
	MaxNameLen  int
	mu          sync.Mutex
}

type EmacsABI struct {
	Version    int
	MinVersion int
}

// getEmacsABI queries Emacs for tree-sitter ABI versions. It returns a struct
// containing the maximum (Version) and minimum (MinVersion) ABI versions Emacs
// supports. Emacs can load any grammar whose parser ABI falls within
// [MinVersion, Version] inclusive.
func (a *App) getEmacsABI(emacsBin string) (EmacsABI, error) {
	expr := `(message "%d %d" (treesit-library-abi-version) (treesit-library-abi-version t))`
	cmd := exec.Command(emacsBin, "--batch", "--eval", expr)
	var out bytes.Buffer
	cmd.Stderr = io.MultiWriter(&out, a.Stderr)
	cmd.Stdout = a.Stdout
	if err := cmd.Run(); err != nil {
		return EmacsABI{}, fmt.Errorf("failed to run Emacs: %w (output: %q)", err, out.String())
	}
	re := regexp.MustCompile(`(\d+)\s+(\d+)`)
	matches := re.FindStringSubmatch(out.String())
	if len(matches) != 3 {
		return EmacsABI{}, fmt.Errorf("could not parse ABI versions from Emacs output: %q", out.String())
	}
	ver, err1 := strconv.Atoi(matches[1])
	minVer, err2 := strconv.Atoi(matches[2])
	if err1 != nil || err2 != nil {
		return EmacsABI{}, fmt.Errorf("invalid ABI version numbers: %s, %s", matches[1], matches[2])
	}
	return EmacsABI{Version: ver, MinVersion: minVer}, nil
}

func (a *App) runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = a.Stdout
	cmd.Stderr = a.Stderr
	return cmd.Run()
}

func (a *App) runCmdWithOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = io.MultiWriter(a.Stdout, &out)
	cmd.Stderr = a.Stderr
	err := cmd.Run()
	return out.Bytes(), err
}

func (a *App) printf(format string, args ...any) {
	fmt.Fprintf(a.Out, format, args...)
}

func (a *App) vprintf(format string, args ...any) {
	if a.Verbose {
		fmt.Fprintf(a.Stdout, format, args...)
	}
}

type LangInfo struct {
	Name      string
	Site      string
	Org       string
	Repo      string
	SourceDir string
	Branch    string
}

func (l LangInfo) GetRepoURL() string {
	site := l.Site
	if site == "" {
		site = "https://github.com"
	}
	org := l.Org
	if org == "" {
		org = "tree-sitter"
	}
	repo := l.Repo
	if repo == "" {
		repo = "tree-sitter-" + l.Name
	}
	return fmt.Sprintf("%s/%s/%s.git", site, org, repo)
}

func (l LangInfo) GetRepoName() string {
	if l.Repo != "" {
		return l.Repo
	}
	return "tree-sitter-" + l.Name
}

func (l LangInfo) GetSourceDir() string {
	if l.SourceDir != "" {
		return l.SourceDir
	}
	return "src"
}

// got this list from tree-sitter-module repo
var languages = []LangInfo{
	{Name: "ada", Org: "briot"},
	{Name: "astro", Org: "virchau13"},
	{Name: "bash"},
	{Name: "bison", Site: "https://gitlab.com", Org: "btuin2"},
	{Name: "c"},
	{Name: "c3", Org: "c3lang"},
	{Name: "c-sharp"},
	{Name: "clojure", Org: "sogaiu"},
	{Name: "cmake", Org: "uyha"},
	{Name: "cpp", Branch: "v0.22.0"},
	{Name: "css"},
	{Name: "cylc", Org: "elliotfontaine"},
	{Name: "dart", Org: "ast-grep"},
	{Name: "dockerfile", Org: "camdencheek"},
	{Name: "doxygen", Org: "tree-sitter-grammars"},
	{Name: "elisp", Org: "Wilfred"},
	{Name: "elixir", Org: "elixir-lang"},
	{Name: "erlang", Org: "WhatsApp"},
	{Name: "glsl", Org: "tree-sitter-grammars"},
	{Name: "go"},
	{Name: "gomod", Org: "camdencheek", Repo: "tree-sitter-go-mod"},
	{Name: "gowork", Org: "omertuc", Repo: "tree-sitter-go-work"},
	{Name: "gpr", Org: "brownts"},
	{Name: "haskell"},
	{Name: "heex", Org: "phoenixframework"},
	{Name: "html"},
	{Name: "janet-simple", Org: "sogaiu"},
	{Name: "java"},
	{Name: "javascript"},
	{Name: "jsdoc"},
	{Name: "json"},
	{Name: "julia"},
	{Name: "kotlin", Org: "fwcd"},
	{Name: "lua", Org: "tree-sitter-grammars"},
	{Name: "magik", Org: "krn-robin"},
	{Name: "make", Org: "tree-sitter-grammars"},
	{Name: "markdown", Org: "tree-sitter-grammars", SourceDir: "tree-sitter-markdown/src"},
	{Name: "markdown-inline", Org: "tree-sitter-grammars", Repo: "tree-sitter-markdown", SourceDir: "tree-sitter-markdown-inline/src"},
	{Name: "nix", Org: "nix-community"},
	{Name: "org", Org: "milisims"},
	{Name: "perl", Org: "ganezdragon"},
	{Name: "php", SourceDir: "php/src"},
	{Name: "proto", Org: "mitchellh"},
	{Name: "python"},
	{Name: "ruby"},
	{Name: "rust"},
	{Name: "scala"},
	{Name: "scss", Org: "tree-sitter-grammars"},
	{Name: "sdml", Org: "sdm-lang"},
	{Name: "souffle", Org: "chaosite"},
	{Name: "sql", Org: "DerekStride", Branch: "gh-pages"},
	{Name: "surface", Org: "connorlay"},
	{Name: "svelte", Org: "Himujjal"},
	{Name: "toml", Org: "tree-sitter-grammars"},
	{Name: "tsx", Repo: "tree-sitter-typescript", SourceDir: "tsx/src"},
	{Name: "typescript", Repo: "tree-sitter-typescript", SourceDir: "typescript/src"},
	{Name: "typst", Org: "uben0"},
	{Name: "vala", Org: "vala-lang"},
	{Name: "verilog", Org: "gmlarumbe"},
	{Name: "vhdl", Org: "alemuller"},
	{Name: "wgsl", Org: "mehmetoguzderin"},
	{Name: "yaml", Org: "tree-sitter-grammars"},
	{Name: "zig", Org: "maxxnino"},
}

func getParserABIVersion(repoPath, sourceDir string) int {
	content, err := os.ReadFile(filepath.Join(repoPath, sourceDir, "parser.c"))
	if err != nil {
		return -1
	}
	re := regexp.MustCompile(`#define\s+LANGUAGE_VERSION\s+(\d+)`)
	match := re.FindStringSubmatch(string(content))
	if len(match) > 1 {
		ver, _ := strconv.Atoi(match[1])
		return ver
	}
	return -1
}

func (a *App) processLanguage(info LangInfo, wg *sync.WaitGroup, sem chan struct{}) {
	defer wg.Done()
	sem <- struct{}{}
	defer func() { <-sem }()

	prefix := fmt.Sprintf("%*s ❚ ", a.MaxNameLen, info.Name)
	la := *a
	la.Out = &prefixWriter{w: a.Out, prefix: prefix, atBOL: true, mu: &a.mu}
	la.Stdout = &prefixWriter{w: a.Stdout, prefix: prefix, atBOL: true, mu: &a.mu}
	la.Stderr = &prefixWriter{w: a.Stderr, prefix: prefix, atBOL: true, mu: &a.mu}

	repoURL := info.GetRepoURL()
	repoPath := filepath.Join(la.CacheDir, info.GetRepoName())

	msg := "[...] Processing"
	if la.Verbose {
		msg += fmt.Sprintf(" (%s)", repoURL)
	}
	la.printf("%s\n", msg)

	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		la.vprintf("    Cloning into %s\n", repoPath)
		gitArgs := []string{"clone", "--quiet", repoURL, repoPath}
		if la.Verbose {
			gitArgs = []string{"clone", repoURL, repoPath}
		}
		if err := la.runCmd("git", gitArgs...); err != nil {
			la.printf("  [!] Error cloning: %v\n", err)
			return
		}
	} else {
		la.vprintf("    Cleaning and fetching latest tags in %s\n", repoPath)
		la.runCmd("git", "-C", repoPath, "reset", "--hard", "HEAD")
		la.runCmd("git", "-C", repoPath, "clean", "-fd")
		gitArgs := []string{"fetch", "--quiet", "--tags"}
		if la.Verbose {
			gitArgs = []string{"fetch", "--tags"}
		}
		la.runCmd("git", append([]string{"-C", repoPath}, gitArgs...)...)
	}

	candidates := []string{}
	if info.Branch != "" {
		candidates = append(candidates, info.Branch)
	}
	candidates = append(candidates, "master", "main")

	tagsOut, _ := la.runCmdWithOutput("git", "-C", repoPath, "tag", "-l", "--sort=-v:refname")
	tags := strings.Split(string(tagsOut), "\n")
	tagCount := 0
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			candidates = append(candidates, tag)
			tagCount++
		}
	}

	la.vprintf("    Found %d tags, total candidates to evaluate: %d\n", tagCount, len(candidates))

	if tagCount == 0 {
		la.vprintf("    No tags found, trying all revisions\n")
		revsOut, _ := la.runCmdWithOutput("git", "-C", repoPath, "log", "--format=%H")
		for _, rev := range strings.Fields(string(revsOut)) {
			candidates = append(candidates, rev)
		}
	}

	foundMatch := false
	var matchedTag string
	matchedABI := -1
	for _, tag := range candidates {
		la.vprintf("    Evaluating candidate: %s\n", tag)
		la.runCmd("git", "-C", repoPath, "reset", "--hard", "HEAD")
		la.runCmd("git", "-C", repoPath, "clean", "-fd")
		checkoutArgs := []string{"checkout", "--force", "--quiet", tag, "--"}
		if la.Verbose {
			checkoutArgs = []string{"checkout", "--force", tag, "--"}
		}
		if err := la.runCmd("git", append([]string{"-C", repoPath}, checkoutArgs...)...); err != nil {
			la.vprintf("    Failed to checkout %s: %v\n", tag, err)
			continue
		}
		currentParserABI := getParserABIVersion(repoPath, info.GetSourceDir())
		la.vprintf("    %s -> ABI %d (accept %d..%d)\n", tag, currentParserABI, la.MinABI, la.TargetABI)
		// Tree-sitter ABIs are backward compatible: Emacs loads any grammar
		// whose parser ABI is within [MinABI, TargetABI]. Candidates are
		// evaluated highest-priority first (explicit branch, then master/main,
		// then newest tags), so the first one inside the window is the best
		// usable revision.
		if currentParserABI >= la.MinABI && currentParserABI <= la.TargetABI {
			foundMatch = true
			matchedTag = tag
			matchedABI = currentParserABI
			break
		}
	}

	if !foundMatch {
		la.printf("  [!] Could not find a parser with ABI in %d..%d. Skipping.\n", la.MinABI, la.TargetABI)
		return
	}

	srcDir := filepath.Join(repoPath, info.GetSourceDir())
	ext := ".so"
	if runtime.GOOS == "darwin" {
		ext = ".dylib"
	} else if runtime.GOOS == "windows" {
		ext = ".dll"
	}
	outPath := filepath.Join(la.OutputDir, "libtree-sitter-"+info.Name+ext)

	hasScannerC := false
	if _, err := os.Stat(filepath.Join(srcDir, "scanner.c")); err == nil {
		hasScannerC = true
	}
	hasScannerCC := false
	if _, err := os.Stat(filepath.Join(srcDir, "scanner.cc")); err == nil {
		hasScannerCC = true
	}

	cc := "gcc"
	cxx := "g++"
	if runtime.GOOS == "darwin" {
		cc = "clang"
		cxx = "clang++"
	}

	objs := []string{}
	parserObj := filepath.Join(la.BuildTmpDir, info.Name+"-parser.o")
	if err := la.runCmd(cc, "-fPIC", "-O2", "-c", "-I", srcDir, filepath.Join(srcDir, "parser.c"), "-o", parserObj); err != nil {
		la.printf("    Error compiling parser.c: %v\n", err)
		return
	}
	objs = append(objs, parserObj)

	if hasScannerC {
		scannerObj := filepath.Join(la.BuildTmpDir, info.Name+"-scanner.o")
		if err := la.runCmd(cc, "-fPIC", "-O2", "-c", "-I", srcDir, filepath.Join(srcDir, "scanner.c"), "-o", scannerObj); err != nil {
			la.printf("    Error compiling scanner.c: %v\n", err)
		} else {
			objs = append(objs, scannerObj)
		}
	}

	if hasScannerCC {
		scannerObj := filepath.Join(la.BuildTmpDir, info.Name+"-scanner-cc.o")
		if err := la.runCmd(cxx, "-fPIC", "-O2", "-c", "-I", srcDir, filepath.Join(srcDir, "scanner.cc"), "-o", scannerObj); err != nil {
			la.printf("    Error compiling scanner.cc: %v\n", err)
		} else {
			objs = append(objs, scannerObj)
		}
	}

	linkCmd := cc
	if hasScannerCC {
		linkCmd = cxx
	}

	args := append([]string{"-shared", "-fPIC", "-o", outPath}, objs...)
	if err := la.runCmd(linkCmd, args...); err != nil {
		la.printf("    Error linking: %v\n", err)
	} else {
		la.printf("  [✓] Installed (%s, ABI %d)\n", matchedTag, matchedABI)
	}

	for _, obj := range objs {
		os.Remove(obj)
	}
}

func main() {
	home, _ := os.UserHomeDir()

	var langNames []string
	for _, l := range languages {
		langNames = append(langNames, l.Name)
	}
	langList := strings.Join(langNames, ", ")

	verboseFlag := flag.Bool("verbose", false, "print detailed information about evaluation")
	workersFlag := flag.Int("workers", 0, "number of workers (default: max(CPU count, 12))")
	langFlag := flag.String("language", "", "comma-separated list of languages to build (available: "+langList+")")
	emacsFlag := flag.String("emacs", "emacs", "emacs binary to use for ABI detection")
	abiFlag := flag.Int("abi", 0, "explicitly set target (max) ABI version (skips detection if > 0)")
	minAbiFlag := flag.Int("min-abi", 0, "explicitly set minimum acceptable ABI version (skips detection if > 0)")
	outFlag := flag.String("out", filepath.Join(home, ".emacs.d", "tree-sitter"), "output directory for built libraries")
	flag.Parse()

	app := &App{
		Verbose:     *verboseFlag,
		CacheDir:    filepath.Join(home, ".cache", "emacs-tree-sitter-manager", "repos"),
		OutputDir:   *outFlag,
		BuildTmpDir: filepath.Join(os.TempDir(), "ts-dynamic-build"),
		Out:         os.Stdout,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	}

	if !app.Verbose {
		app.Stdout = io.Discard
		app.Stderr = io.Discard
	}

	app.TargetABI = *abiFlag
	app.MinABI = *minAbiFlag
	if app.TargetABI <= 0 || app.MinABI <= 0 {
		abis, err := app.getEmacsABI(*emacsFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error detecting Emacs tree-sitter ABI: %v\n", err)
			os.Exit(1)
		}
		if app.TargetABI <= 0 {
			app.TargetABI = abis.Version
		}
		if app.MinABI <= 0 {
			app.MinABI = abis.MinVersion
		}
	}
	if app.MinABI > app.TargetABI {
		app.MinABI = app.TargetABI
	}
	app.printf("Accepting parser ABI %d..%d\n", app.MinABI, app.TargetABI)

	os.MkdirAll(app.OutputDir, 0o755)
	os.MkdirAll(app.CacheDir, 0o755)
	os.MkdirAll(app.BuildTmpDir, 0o755)

	var targets []LangInfo
	if *langFlag != "" {
		requested := strings.Split(*langFlag, ",")
		reqMap := make(map[string]bool)
		for _, r := range requested {
			reqMap[strings.TrimSpace(r)] = true
		}
		for _, info := range languages {
			if reqMap[info.Name] {
				targets = append(targets, info)
			}
		}
		if len(targets) == 0 {
			app.printf("No valid languages found in: %s\n", *langFlag)
			os.Exit(1)
		}
	} else {
		targets = languages
	}

	maxNameLen := 0
	for _, info := range targets {
		if len(info.Name) > maxNameLen {
			maxNameLen = len(info.Name)
		}
	}
	app.MaxNameLen = maxNameLen

	var wg sync.WaitGroup
	numWorkers := *workersFlag
	if numWorkers <= 0 {
		numWorkers = max(runtime.NumCPU(), 12)
	}
	sem := make(chan struct{}, numWorkers)

	for _, info := range targets {
		wg.Add(1)
		go app.processLanguage(info, &wg, sem)
	}

	wg.Wait()
	app.printf("All done.\n")
}

type prefixWriter struct {
	w      io.Writer
	prefix string
	atBOL  bool
	mu     *sync.Mutex
}

func (pw *prefixWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	pw.mu.Lock()
	defer pw.mu.Unlock()

	var buf bytes.Buffer
	for _, b := range p {
		if pw.atBOL {
			buf.WriteString(pw.prefix)
			pw.atBOL = false
		}
		buf.WriteByte(b)
		if b == '\n' {
			pw.atBOL = true
		}
	}
	_, err = pw.w.Write(buf.Bytes())
	return len(p), err
}
