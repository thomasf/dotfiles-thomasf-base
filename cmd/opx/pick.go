package main

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	clearAfter = 45 * time.Second // a copied secret stays in the clipboard, like pass -c
	resumeFor  = 5 * time.Minute  // the next field of the last item is preselected
	otpPeriod  = 30 * time.Second
	otpMinLeft = 5 * time.Second // wait for the next code below this
)

type action int

const (
	actCopy action = iota
	actType
)

var errCancelled = errors.New("cancelled")

type menuRequest struct {
	Prompt   string
	Lines    []string
	Selected int
	Message  string
	TypeKey  bool // Ctrl+Enter returns actType
}

// pick picks an item and a field with rofi and copies or types it.
func pick(ctx context.Context, profile Profile) error {
	if err := checkDisplay(); err != nil {
		return err
	}
	s, err := newSessions(profile)
	if err != nil {
		return err
	}
	s.ask = rofiPassword
	lastPath := s.file("last")
	last := loadLastUse(lastPath)

	var items []ListItem
	if err := opJSON(ctx, s, &items, "item", "list", "--format=json"); err != nil {
		return err
	}
	if len(items) == 0 {
		return errors.New("no items")
	}
	sortItems(items, last.ItemID)
	lines := make([]string, len(items))
	for i, it := range items {
		lines[i] = fmt.Sprintf("%s  (%s)", oneLine(it.Title), it.Vault.Name)
	}
	i, _, err := rofiMenu(ctx, menuRequest{Prompt: cmp.Or(profile.Name, "op"), Lines: lines})
	if err != nil {
		return err
	}

	var item Item
	if err := opJSON(ctx, s, &item, "item", "get", items[i].ID, "--format=json"); err != nil {
		return err
	}
	fields := menuFields(item)
	if len(fields) == 0 {
		return fmt.Errorf("%s has no fields", item.Title)
	}
	f, act := 0, actCopy
	if len(fields) > 1 {
		lines := make([]string, len(fields))
		for i, mf := range fields {
			lines[i] = mf.line()
		}
		f, act, err = rofiMenu(ctx, menuRequest{
			Prompt:   item.Title,
			Lines:    lines,
			Selected: last.next(item.ID, len(fields), time.Now()),
			Message:  "Enter: copy    Ctrl+Enter: type",
			TypeKey:  true,
		})
		if err != nil {
			return err
		}
	}
	mf := fields[f]
	saveLastUse(lastPath, lastUse{item.ID, f, time.Now()})

	value := mf.Value
	var notes []string
	if mf.Kind == kindOTP {
		wait, valid := otpWait(time.Now())
		if wait > 0 {
			if err := sleepCtx(ctx, wait); err != nil {
				return err
			}
			out, err := s.run(ctx, "item", "get", item.ID, "--otp")
			if err != nil {
				return err
			}
			value = strings.TrimSpace(string(out))
		}
		notes = append(notes, fmt.Sprintf("valid for %ds", int(valid.Seconds())))
	}

	if act == actType {
		if err := typeText(value); err != nil {
			return fmt.Errorf("type: %w", err)
		}
		return nil
	}
	if mf.Kind == kindPlain {
		err = copyToClipboard(value)
	} else {
		err = copySecret(value, clearAfter)
		notes = append(notes, fmt.Sprintf("clears in %ds", int(clearAfter.Seconds())))
	}
	if err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	msg := fmt.Sprintf("Copied %s of %s", mf.Label, item.Title)
	if len(notes) > 0 {
		msg += " (" + strings.Join(notes, ", ") + ")"
	}
	notify(false, msg)
	return nil
}

// opJSON runs op with args and decodes its output into v.
func opJSON(ctx context.Context, s *sessions, v any, args ...string) error {
	out, err := s.run(ctx, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("op %s %s: %w", args[0], args[1], err)
	}
	return nil
}

// sortItems puts lastID first, the rest by title.
func sortItems(items []ListItem, lastID string) {
	slices.SortStableFunc(items, func(a, b ListItem) int {
		if (a.ID == lastID) != (b.ID == lastID) {
			if a.ID == lastID {
				return -1
			}
			return 1
		}
		return cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})
}

// otpWait returns how long to wait for a code that is not about to expire,
// and how long that code is valid.
func otpWait(now time.Time) (wait, valid time.Duration) {
	left := otpPeriod - time.Duration(now.UnixNano()%int64(otpPeriod))
	if left < otpMinLeft {
		return left, otpPeriod
	}
	return 0, left
}

type lastUse struct {
	ItemID string
	Field  int
	Time   time.Time
}

// next is the field to preselect in itemID's field menu of n fields.
func (l lastUse) next(itemID string, n int, now time.Time) int {
	if l.ItemID != itemID || n == 0 || now.Before(l.Time) || now.Sub(l.Time) >= resumeFor {
		return 0
	}
	return (l.Field + 1) % n
}

func loadLastUse(path string) lastUse {
	var l lastUse
	b, err := os.ReadFile(path)
	if err != nil {
		return l
	}
	var unix int64
	if _, err := fmt.Sscan(string(b), &l.ItemID, &l.Field, &unix); err != nil {
		return lastUse{}
	}
	l.Time = time.Unix(unix, 0)
	return l
}

func saveLastUse(path string, l lastUse) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	os.WriteFile(path, fmt.Appendf(nil, "%s %d %d\n", l.ItemID, l.Field, l.Time.Unix()), 0o600)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
