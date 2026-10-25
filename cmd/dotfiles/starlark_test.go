package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.starlark.net/starlark"
)

func TestDecodeDocument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		format, in string
		want       string // Starlark repr
		wantErr    string
	}{
		{format: formatJSON, in: `{"b": 1, "a": [1.0, 2e3, null, true, "s", 12345678901234567890]}`, want: `{"a": [1.0, 2000.0, None, True, "s", 12345678901234567890], "b": 1}`},
		{format: formatJSON, in: "  \n", want: "{}"},
		{format: formatJSON, in: `[1]`, wantErr: "not a map"},
		{format: formatJSON, in: `{"a": 1} {}`, wantErr: "after the top-level value"},
		{format: formatJSON, in: `{"a": `, wantErr: "EOF"},
		{format: formatTOML, in: "b = 1\na = 1.0\n[t]\nx = 'y'\n", want: `{"a": 1.0, "b": 1, "t": {"x": "y"}}`},
		{format: formatTOML, in: "d = 2001-12-14\n", wantErr: "dates and times"},
	}
	for _, tt := range tests {
		doc, err := decodeDocument(tt.format, []byte(tt.in))
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s %q: got error %v, want %q", tt.format, tt.in, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s %q: %v", tt.format, tt.in, err)
			continue
		}
		if got := doc.String(); got != tt.want {
			t.Errorf("%s %q: got %s, want %s", tt.format, tt.in, got, tt.want)
		}
	}
}

func TestEncodeDocument(t *testing.T) {
	t.Parallel()
	value := func(src string) *starlark.Dict {
		v, err := starlark.EvalOptions(starlarkFileOptions, &starlark.Thread{}, "test", src, nil)
		if err != nil {
			t.Fatal(err)
		}
		return v.(*starlark.Dict)
	}
	tests := []struct {
		format  string
		value   string
		want    string
		wantErr string
	}{
		{format: formatJSON, value: `{"b": 1, "a": {"x": [1.0, None, "<&>", 12345678901234567890], "e": {}, "l": []}}`, want: `{
  "a": {
    "e": {},
    "l": [],
    "x": [
      1.0,
      null,
      "<&>",
      12345678901234567890
    ]
  },
  "b": 1
}
`},
		{format: formatJSON, value: `{"f": float("inf")}`, wantErr: "f: JSON cannot represent"},
		{format: formatJSON, value: `{1: 2}`, wantErr: "not a string"},
		{format: formatTOML, value: `{"b": 1, "a": 2.0, "t": [{"x": 1}]}`, want: "a = 2.0\nb = 1\n\n[[t]]\nx = 1\n"},
		{format: formatTOML, value: `{"a": {"b": None}}`, wantErr: "a.b: TOML has no null"},
	}
	for _, tt := range tests {
		out, err := encodeDocument(tt.format, value(tt.value), 2)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s %s: got error %v, want %q", tt.format, tt.value, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s %s: %v", tt.format, tt.value, err)
			continue
		}
		if string(out) != tt.want {
			t.Errorf("%s %s: got\n%s\nwant\n%s", tt.format, tt.value, out, tt.want)
		}
		// What is written reads back as the same value.
		doc, err := decodeDocument(tt.format, out)
		if err != nil {
			t.Errorf("%s %s: decoding the output: %v", tt.format, tt.value, err)
		} else if !valuesEqual(doc, value(tt.value)) {
			t.Errorf("%s %s: reads back as %s", tt.format, tt.value, doc)
		}
	}
}

func TestMergePatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		target, patch, want string
	}{
		{`{"a": 1, "b": 2}`, `{"b": 3, "c": 4}`, `{"a": 1, "b": 3, "c": 4}`},
		{`{"a": 1, "b": 2}`, `{"a": None, "x": None}`, `{"b": 2}`},
		{`{"a": {"x": 1, "y": 2}}`, `{"a": {"y": None, "z": 3}}`, `{"a": {"x": 1, "z": 3}}`},
		{`{"a": 1}`, `{"a": {"n": None, "m": 1}}`, `{"a": {"m": 1}}`},
		{`{"a": [1, 2]}`, `{"a": [None]}`, `{"a": [None]}`},
	}
	for _, tt := range tests {
		code := "t = " + tt.target + "\np = " + tt.patch + "\nmerge(t, p)\nmerge(t, p)\n"
		globals, err := starlark.ExecFileOptions(starlarkFileOptions, &starlark.Thread{}, "test", code, starlark.StringDict{"merge": mergeBuiltin})
		if err != nil {
			t.Errorf("%s: %v", code, err)
			continue
		}
		if got := globals["t"].String(); got != tt.want {
			t.Errorf("merge(%s, %s): got %s, want %s", tt.target, tt.patch, got, tt.want)
		}
	}

	// The target does not share lists with the patch.
	code := "t = {}\np = {\"l\": [1]}\nmerge(t, p)\np[\"l\"].append(2)\n"
	globals, err := starlark.ExecFileOptions(starlarkFileOptions, &starlark.Thread{}, "test", code, starlark.StringDict{"merge": mergeBuiltin})
	if err != nil {
		t.Fatal(err)
	}
	if got := globals["t"].String(); got != `{"l": [1]}` {
		t.Errorf("target shares a list with the patch: %s", got)
	}
}

func TestStarlarkConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		config  string
		wantErr string
	}{
		{config: "[[starlark]]\ndst = 'a.json'\n", wantErr: "starlark 0: src is required"},
		{config: "[[starlark]]\nsrc = 'pass'\n", wantErr: "starlark 0: dst is required"},
		{config: "[[starlark]]\ndst = 'a.json'\nsrc = 'pass'\nmissing = 'crate'\n", wantErr: `invalid missing "crate"`},
		{config: "[[starlark]]\ndst = 'a.json'\nsrc = 'pass'\nformat = 'ini'\n", wantErr: `invalid format "ini"`},
		{config: "[[starlark]]\ndst = 'a.json'\nsrc = 'pass'\nindent = 10\n", wantErr: "invalid indent 10"},
		{config: "[[starlark]]\ndst = 'a.json'\nsrc = 'pass'\nmode = '999'\n", wantErr: `invalid mode "999"`},
		{config: "[[starlark]]\ndst = 'a.json'\nsrc = 'pass'\nscript = 'x'\n", wantErr: "script"},
		{config: "[[starlark]]\ndst = 'a.json'\nsrc = '''\nx = 1\nhostnme\n'''\n", wantErr: "starlark[0].src:2:1: undefined: hostnme"},
		{config: "[[starlark]]\ndst = 'a.json'\nsrc = 'if'\n", wantErr: "starlark[0].src:1:3"},
		{config: "[[starlark]]\ndst = '*.json'\nsrc = 'pass'\nmissing = 'create'\n", wantErr: "a glob cannot be used"},
	}
	for _, tt := range tests {
		repoDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(repoDir, "dotfiles.toml"), []byte(tt.config), 0o644); err != nil {
			t.Fatal(err)
		}
		err := NewRepository(repoDir, t.TempDir()).LoadConfig()
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: got error %v, want %q", tt.config, err, tt.wantErr)
		}
	}
}

func TestStarlarkDstTemplates(t *testing.T) {
	t.Parallel()
	repo := starlarkRepo(t, `
[[starlark]]
dst = ['{{.os}}.json', '{{if false}}never.json{{end}}']
src = 'pass'

[[starlark]]
dst = '{{if false}}never.json{{end}}'
src = 'pass'

[[starlark]]
dst = 'skipped.json'
src = 'pass'
condition = "os == 'plan9-nope'"
`, nil, nil)
	actions, err := repo.Sync()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range actions {
		if ta, ok := a.(*StarlarkAction); ok {
			got = append(got, ta.String())
		}
	}
	want := "[" + filepath.Base(repo.srcRoot) + "] starlark: " + runtime.GOOS + ".json"
	if len(got) != 1 || got[0] != want {
		t.Errorf("got actions %q, want [%q]", got, want)
	}
}

// TestStarlarkBuiltins checks that the built-ins the README lists work in
// the Starlark dialect of [[starlark]] entries.
func TestStarlarkBuiltins(t *testing.T) {
	t.Parallel()
	code := `
d = {"a": 1, "l": [1]}
d.get("x", 0)
d.setdefault("n", {})["k"] = 1
d.pop("x", None)
d.update({"b": 2})
for k, v in d.items():
    pass
[d.keys(), d.values()]
l = d["l"]
l.append(2)
l.extend([3])
l.remove(3)
ok = 2 in l and "a" in d
n = int("4") + len(l)
f = float("1.5")
s = str(n)
for i, x in enumerate(sorted([3, 1]) + list(range(2))):
    pass
w = ",".join("a b".split()) + " x ".strip()
w.startswith("a")

def twice(x):
    return x * 2

n = twice(n)
d.clear()
`
	prog, err := compileStarlark("test", code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prog.Init(&starlark.Thread{}, starlarkPredeclared("", "", evalContext{}, starlark.NewDict(0))); err != nil {
		t.Fatal(err)
	}
	if _, err := compileStarlark("test", "while True:\n    pass\n"); err == nil {
		t.Error("while is allowed")
	}
	if _, err := compileStarlark("test", "x = 1\nload(\"x.star\", \"y\")\n"); err == nil || !strings.Contains(err.Error(), "test:2:6: load is not supported") {
		t.Errorf("load: got error %v", err)
	}
}

func TestStarlarkErrorMessages(t *testing.T) {
	t.Parallel()
	// All resolve errors are shown, and a caret after a tab lines up.
	_, err := compileStarlark("s", "merge(dok, {})\nif True:\n\tdoc[\"a\"] = sizes\n")
	want := `s:1:7: undefined: dok
  1 | merge(dok, {})
    |       ^
s:3:13: undefined: sizes
  1 | merge(dok, {})
  2 | if True:
  3 | 	doc["a"] = sizes
    | 	           ^`
	if err == nil || err.Error() != want {
		t.Errorf("compile: got\n%v\nwant\n%s", err, want)
	}

	// A runtime error shows the line that failed and the lines it was called
	// from, but not built-ins.
	src := "def f(x):\n    return int(x)\n\ny = f(\"a\")\n"
	prog, err := compileStarlark("s", src)
	if err != nil {
		t.Fatal(err)
	}
	_, err = prog.Init(&starlark.Thread{}, starlarkPredeclared("", "", evalContext{}, starlark.NewDict(0)))
	want = `s:2:15: int: invalid literal with base 10: a
  1 | def f(x):
  2 |     return int(x)
    |               ^
s:4:6: called from here
  1 | def f(x):
  2 |     return int(x)
  3 |
  4 | y = f("a")
    |      ^`
	if err = starlarkError(src, err); err == nil || err.Error() != want {
		t.Errorf("runtime: got\n%v\nwant\n%s", err, want)
	}

	// At most three lines are shown before the error, and line numbers are
	// right-aligned.
	_, err = compileStarlark("s", strings.Repeat("x = 1\n", 9)+"y = z\n")
	want = `s:10:5: undefined: z
   7 | x = 1
   8 | x = 1
   9 | x = 1
  10 | y = z
     |     ^`
	if err == nil || err.Error() != want {
		t.Errorf("line 10: got\n%v\nwant\n%s", err, want)
	}
}
