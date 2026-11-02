package server

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orgzd/internal/agenda"
	"orgzd/internal/org"
)

func TestNestedAgendaTemplate(t *testing.T) {
	dir := t.TempDir()
	for _, file := range []string{"work.org", "inbox.org"} {
		planning := ""
		if file == "work.org" {
			planning = "SCHEDULED: <2026-11-02 Mon>\n"
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte("* Parent\n"+planning+"** TODO Child <unsafe>\n*** DONE Grandchild\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := org.ParseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	data := struct {
		DateStr       string
		ConflictCount int
		Inbox         []*org.Entry
		Groups        []agenda.Group
	}{Groups: agenda.Build(entries, time.Date(2026, 11, 2, 0, 0, 0, 0, time.Local))}
	for _, e := range entries {
		if e.File == "inbox.org" && e.Parent == nil {
			data.Inbox = append(data.Inbox, e)
		}
	}
	var buf bytes.Buffer
	if err := agendaTmpl.Execute(&buf, data); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if strings.Count(html, `class="e child"`) != 4 || strings.Count(html, `class="toinbox"`) != 1 || strings.Count(html, `class="qs"`) != 2 || strings.Contains(html, `<details class="e" open`) || strings.Contains(html, "<unsafe>") {
		t.Fatal("nested rendering or root-only controls incorrect")
	}
	child := entries[1]
	draft, err := org.LoadEntry(dir, child.File, child.Line)
	if err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := editTmpl.Execute(&buf, editFormData{Mode: "edit", Draft: draft}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), `name="scheduled" value=`) || !strings.Contains(buf.String(), "top-level task") {
		t.Fatal("child editor exposes dates")
	}
}
