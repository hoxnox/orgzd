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
