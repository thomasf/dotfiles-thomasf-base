package main

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"testing"
)

type wantField struct {
	label string
	kind  fieldKind
	value string
}

func TestMenuFields(t *testing.T) {
	for name, tc := range map[string]struct {
		item []byte
		want []wantField
	}{
		"login": {itemLoginJSON, []wantField{
			{"username", kindPlain, "fake-user"},
			{"password", kindSecret, "fake-pass\twith-tab"},
			{"Section A › one-time password", kindOTP, "000000"},
			{"url", kindPlain, "https://one.example"},
			{"url", kindPlain, "https://two.example"},
		}},
		"password": {itemPasswordJSON, []wantField{
			{"password", kindSecret, "fake-pass-two"},
		}},
		"api-credential": {itemAPICredentialJSON, []wantField{
			{"notes", kindPlain, "line one\nline two"},
			{"username", kindPlain, "fake-user-three"},
			{"credential", kindSecret, "fake-credential"},
			{"Section A › secret a", kindSecret, "fake-secret-a"},
			{"Section A › text a", kindPlain, "fake-text-a"},
		}},
		"ssh-key": {itemSSHKeyJSON, []wantField{
			{"private key", kindSecret, "FAKE PRIVATE KEY\n"},
			{"public key", kindPlain, "fake-public-key"},
		}},
		"identity": {itemIdentityJSON, []wantField{
			{"Section A › text b", kindPlain, "fake-text-b"},
			{"Section B › email b", kindPlain, "fake@example.invalid"},
			{"fieldstr02", kindPlain, "fake-unlabeled"},
			{"fieldurl01", kindPlain, "https://three.example"},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			var item Item
			if err := json.Unmarshal(tc.item, &item); err != nil {
				t.Fatal(err)
			}
			var got []wantField
			for _, f := range menuFields(item) {
				got = append(got, wantField{f.Label, f.Kind, f.Value})
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func TestOneLine(t *testing.T) {
	for in, want := range map[string]string{
		"a\tb\nc":                "a b",
		string(make([]rune, 61)): string(make([]rune, 60)) + "…",
	} {
		if got := oneLine(in); got != want {
			t.Errorf("oneLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMenuFieldLineMasksSecrets(t *testing.T) {
	for _, f := range []menuField{
		{Label: "password", Kind: kindSecret, Value: "fake-pass"},
		{Label: "one-time password", Kind: kindOTP, Value: "000000"},
	} {
		if got, want := f.line(), fmt.Sprintf("%-24s %s", f.Label, masked); got != want {
			t.Errorf("line %q, want %q", got, want)
		}
	}
	if got := (menuField{Label: "username", Value: "fake-user"}).line(); !strings.HasSuffix(got, " fake-user") {
		t.Errorf("plain field line %q", got)
	}
}
