package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// configDir writes files, relative paths to contents, into a new directory.
func configDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const (
	profileA = "account = \"a.example\"\npassword_command = [\"fake-pass\", \"show\", \"a\"]\n"
	profileB = "account = \"b.example\"\n"
)

func TestLoadProfile(t *testing.T) {
	dir := configDir(t, map[string]string{
		"config.toml":     "default_profile = \"b\"\n",
		"profiles/a.toml": profileA,
		"profiles/b.toml": profileB,
	})

	p, err := loadProfile(dir, "a")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "a" || p.Account != "a.example" || !slices.Equal(p.PasswordCommand, []string{"fake-pass", "show", "a"}) {
		t.Errorf("-p a: got %+v", p)
	}

	p, err = loadProfile(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "b" || p.Account != "b.example" {
		t.Errorf("default: got %+v", p)
	}
}

func TestLoadProfileOnlyOne(t *testing.T) {
	dir := configDir(t, map[string]string{"profiles/a.toml": profileA})
	p, err := loadProfile(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "a" {
		t.Errorf("got %+v, want the only profile", p)
	}
}

func TestLoadProfileNone(t *testing.T) {
	p, err := loadProfile(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "" || p.Account != "" || p.PasswordCommand != nil {
		t.Errorf("got %+v, want zero Profile", p)
	}
}

func TestLoadProfileErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		files   map[string]string
		profile string
		want    string
	}{
		"several, no default": {
			files: map[string]string{"profiles/a.toml": profileA, "profiles/b.toml": profileB},
			want:  "set default_profile",
		},
		"unknown -p": {
			files:   map[string]string{"profiles/a.toml": profileA},
			profile: "c",
			want:    `no profile "c"`,
		},
		"unknown key in profile": {
			files: map[string]string{"profiles/a.toml": "acount = \"x\"\n"},
			want:  "unknown keys",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadProfile(configDir(t, tc.files), tc.profile)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err %v, want %q", err, tc.want)
			}
		})
	}
}
