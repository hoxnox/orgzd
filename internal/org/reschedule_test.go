package org

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRescheduleAllInsertsScheduledForUndated(t *testing.T) {
	dir := t.TempDir()
	content := "#+TODO: TODO WAIT | DONE CANCELED\n\n* TODO First task :vk:\n* TODO Second task\nsome body\n* TODO Third task\n"
	if err := os.WriteFile(filepath.Join(dir, "inbox.org"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	date := time.Date(2026, 7, 2, 0, 0, 0, 0, time.Local)
	// all three at once: insertions must not shift the later refs
	refs := []ScheduleRef{
		{File: "inbox.org", Line: 3},
		{File: "inbox.org", Line: 4},
		{File: "inbox.org", Line: 6},
	}
	if err := RescheduleAll(dir, refs, date); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "inbox.org"))
	got := string(data)
	want := "#+TODO: TODO WAIT | DONE CANCELED\n\n" +
		"* TODO First task :vk:\nSCHEDULED: <2026-07-02 Thu>\n" +
		"* TODO Second task\nSCHEDULED: <2026-07-02 Thu>\nsome body\n" +
		"* TODO Third task\nSCHEDULED: <2026-07-02 Thu>\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	entries, err := ParseFile(filepath.Join(dir, "inbox.org"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Scheduled == nil || !e.Scheduled.Time.Equal(date) {
			t.Errorf("%q: scheduled = %v", e.Title, e.Scheduled)
		}
	}
}

func TestRescheduleAllPrependsToExistingPlanningLine(t *testing.T) {
	dir := t.TempDir()
	content := "* TODO Deadline only\nDEADLINE: <2026-07-10 Fri>\n"
	if err := os.WriteFile(filepath.Join(dir, "a.org"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 7, 2, 0, 0, 0, 0, time.Local)
	if err := RescheduleAll(dir, []ScheduleRef{{File: "a.org", Line: 1}}, date); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "a.org"))
	if !strings.Contains(string(data), "SCHEDULED: <2026-07-02 Thu> DEADLINE: <2026-07-10 Fri>") {
		t.Errorf("planning line wrong:\n%s", data)
	}
}

func TestScheduleMoveMovesToTargetWithSchedule(t *testing.T) {
	dir := t.TempDir()
	inbox := "#+TODO: TODO WAIT | DONE CANCELED\n\n* TODO First :vk:\nbody first\n\n* TODO Second :vk:\nbody second\n\n* TODO Third\n"
	work := "#+TODO: TODO WAIT | DONE CANCELED\n\n* Existing\nSCHEDULED: <2026-07-01 Wed>\n"
	os.WriteFile(filepath.Join(dir, "inbox.org"), []byte(inbox), 0644)
	os.WriteFile(filepath.Join(dir, "work.org"), []byte(work), 0644)

	date := time.Date(2026, 7, 9, 0, 0, 0, 0, time.Local)
	// mixed targets in one call, ascending line order on purpose
	refs := []MoveScheduleRef{
		{File: "inbox.org", Line: 3, Target: "work.org"},
		{File: "inbox.org", Line: 6, Target: "home.org"}, // home.org doesn't exist yet
		{File: "inbox.org", Line: 9, Target: "inbox.org"},
	}
	if err := ScheduleMove(dir, refs, date); err != nil {
		t.Fatal(err)
	}

	ib, _ := os.ReadFile(filepath.Join(dir, "inbox.org"))
	if strings.Contains(string(ib), "First") || strings.Contains(string(ib), "Second") {
		t.Errorf("moved entries must leave inbox:\n%s", ib)
	}
	if !strings.Contains(string(ib), "* TODO Third\nSCHEDULED: <2026-07-09 Thu>") {
		t.Errorf("same-file target must schedule in place:\n%s", ib)
	}

	wk, _ := os.ReadFile(filepath.Join(dir, "work.org"))
	if !strings.Contains(string(wk), "* Existing") ||
		!strings.Contains(string(wk), "* TODO First :vk:\nSCHEDULED: <2026-07-09 Thu>\nbody first") {
		t.Errorf("work.org must keep old content and gain scheduled entry with body:\n%s", wk)
	}

	hm, _ := os.ReadFile(filepath.Join(dir, "home.org"))
	if !strings.Contains(string(hm), "* TODO Second :vk:\nSCHEDULED: <2026-07-09 Thu>\nbody second") {
		t.Errorf("home.org must be created with the moved entry:\n%s", hm)
	}

	// everything still parses and is scheduled
	entries, err := ParseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]*Entry{}
	for _, e := range entries {
		byTitle[e.Title] = e
	}
	for title, wantFile := range map[string]string{"First": "work.org", "Second": "home.org", "Third": "inbox.org"} {
		e := byTitle[title]
		if e == nil || e.File != wantFile {
			t.Errorf("%s: entry = %+v, want in %s", title, e, wantFile)
		} else if e.Scheduled == nil || !e.Scheduled.Time.Equal(date) {
			t.Errorf("%s: scheduled = %v", title, e.Scheduled)
		}
	}
}

func TestScheduleMoveValidatesTarget(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "inbox.org"), []byte("* TODO X\n"), 0644)
	date := time.Date(2026, 7, 9, 0, 0, 0, 0, time.Local)
	for _, bad := range []string{"../evil.org", "sub/dir.org", "notes.txt"} {
		if err := ScheduleMove(dir, []MoveScheduleRef{{File: "inbox.org", Line: 1, Target: bad}}, date); err == nil {
			t.Errorf("target %q must be rejected", bad)
		}
	}
}

func TestRescheduleAllStillReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	content := "* TODO Dated\nSCHEDULED: <2026-06-01 Mon 10:30 +1w>\n"
	if err := os.WriteFile(filepath.Join(dir, "a.org"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 7, 2, 0, 0, 0, 0, time.Local)
	if err := RescheduleAll(dir, []ScheduleRef{{File: "a.org", Line: 1}}, date); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "a.org"))
	if !strings.Contains(string(data), "SCHEDULED: <2026-07-02 Thu 10:30 +1w>") {
		t.Errorf("time-of-day and repeater must be preserved:\n%s", data)
	}
}
