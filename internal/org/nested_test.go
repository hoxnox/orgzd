package org

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNestedEntriesAndMutations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "work.org")
	source := "* Project\nSCHEDULED: <2026-11-02 Mon>\nparent body\n** TODO Child\nSCHEDULED: <2020-01-01 Wed>\nchild body\n**** DONE Grandchild\n* Other\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	entries, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	root, child, grandchild := entries[0], entries[1], entries[2]
	if len(root.Children) != 1 || child.Parent != root || grandchild.Parent != child || entries[3].Parent != nil {
		t.Fatal("incorrect hierarchy")
	}
	if grandchild.Root() != root || child.RelevantDate() != root.Scheduled {
		t.Fatal("date must come from root")
	}
	for name, action := range map[string]func() error{
		"inbox": func() error { return MoveToInbox(dir, "work.org", child.Line) },
		"schedule": func() error {
			return RescheduleAll(dir, []ScheduleRef{{"work.org", root.Line}, {"work.org", child.Line}}, time.Now())
		},
		"move": func() error {
			return ScheduleMove(dir, []MoveScheduleRef{{"work.org", root.Line, "inbox.org"}, {"work.org", child.Line, "inbox.org"}}, time.Now())
		},
	} {
		if action() == nil {
			t.Fatalf("%s accepted child", name)
		}
		data, _ := os.ReadFile(path)
		if string(data) != source {
			t.Fatalf("%s changed source on rejection", name)
		}
	}
	draft, err := LoadEntry(dir, "work.org", child.Line)
	if err != nil {
		t.Fatal(err)
	}
	if draft.ParentLine != root.Line || draft.ParentScheduled != "2026-11-02" {
		t.Fatalf("missing inherited planning: %+v", draft)
	}
	draft.Title = "Edited child"
	draft.Scheduled = "2030-01-01"
	if err := UpdateEntry(dir, draft); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "** TODO Edited child\nSCHEDULED: <2020-01-01 Wed>") || !strings.Contains(string(data), "**** DONE Grandchild") {
		t.Fatalf("child edit lost source: %s", data)
	}
	if err := MoveToInbox(dir, "work.org", 1); err != nil {
		t.Fatal(err)
	}
	inbox, err := ParseFile(filepath.Join(dir, "inbox.org"))
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 3 || inbox[0].Scheduled != nil || inbox[1].Scheduled == nil || inbox[2].Root() != inbox[0] {
		t.Fatal("inbox move broke subtree")
	}
	if err := ScheduleMove(dir, []MoveScheduleRef{{"inbox.org", inbox[0].Line, "work.org"}}, time.Date(2026, 11, 3, 0, 0, 0, 0, time.Local)); err != nil {
		t.Fatal(err)
	}
	entries, err = ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 || entries[1].Title != "Project" || entries[3].Root() != entries[1] || entries[2].RelevantDate().Time.Day() != 3 {
		t.Fatal("schedule move broke subtree")
	}
}
