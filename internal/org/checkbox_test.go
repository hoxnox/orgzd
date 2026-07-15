package org

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToggleCheckbox(t *testing.T) {
	dir := t.TempDir()
	content := `* TODO Shopping
SCHEDULED: <2026-07-14 Tue>
:PROPERTIES:
:CREATED: [2026-07-01 Wed]
:END:
intro text
- [ ] milk
- [x] bread
  + [ ] nested butter
1. [-] partially
- plain item, not a checkbox

* Next entry
- [ ] belongs to next entry
`
	path := filepath.Join(dir, "home.org")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// check #0 (milk)
	if err := ToggleCheckbox(dir, "home.org", 1, 0, true); err != nil {
		t.Fatal(err)
	}
	// uncheck #1 (bread)
	if err := ToggleCheckbox(dir, "home.org", 1, 1, false); err != nil {
		t.Fatal(err)
	}
	// check #3 (partially, numbered bullet)
	if err := ToggleCheckbox(dir, "home.org", 1, 3, true); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	for _, want := range []string{"- [x] milk", "- [ ] bread", "  + [ ] nested butter", "1. [x] partially", "- [ ] belongs to next entry"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}

	// index past the entry's checkboxes must not leak into the next entry
	if err := ToggleCheckbox(dir, "home.org", 1, 4, true); err == nil {
		t.Error("index beyond entry must fail")
	}
	// not a headline
	if err := ToggleCheckbox(dir, "home.org", 2, 0, true); err == nil {
		t.Error("non-headline line must fail")
	}
	// path traversal
	if err := ToggleCheckbox(dir, "../home.org", 1, 0, true); err == nil {
		t.Error("path traversal must be rejected")
	}
}

func TestToggleCheckboxSkipsDrawerContent(t *testing.T) {
	dir := t.TempDir()
	content := `* Task
:LOGBOOK:
- [ ] looks like a checkbox but lives in a drawer
:END:
- [ ] real one
`
	path := filepath.Join(dir, "a.org")
	os.WriteFile(path, []byte(content), 0644)
	if err := ToggleCheckbox(dir, "a.org", 1, 0, true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "- [x] real one") ||
		!strings.Contains(string(data), "- [ ] looks like a checkbox") {
		t.Errorf("drawer content must be skipped:\n%s", data)
	}
}
