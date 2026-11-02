package agenda

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"orgzd/internal/org"
)

func TestBuildGroupsOnlyRoots(t *testing.T) {
	dir := t.TempDir()
	source := "* Project\nSCHEDULED: <2026-11-02 Mon>\n** TODO Overdue child\nSCHEDULED: <2020-01-01 Wed>\n*** TODO Future grandchild\nDEADLINE: <2030-01-01 Tue>\n* Undated\n** TODO Dated child\nSCHEDULED: <2026-11-02 Mon>\n* DONE Completed\nSCHEDULED: <2026-11-02 Mon>\n** TODO Still open\nSCHEDULED: <2026-11-02 Mon>\n"
	if err := os.WriteFile(filepath.Join(dir, "work.org"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "home.org"), []byte("* Other file\nDEADLINE: <2026-11-03 Tue>\n"), 0644); err != nil {
		t.Fatal(err)
	}
	entries, err := org.ParseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	groups := Build(entries, time.Date(2026, 11, 2, 0, 0, 0, 0, time.Local))
	if len(groups) != 2 || groups[0].Type != "today" || len(groups[0].Entries) != 1 || groups[0].Entries[0].Title != "Project" || len(groups[0].Entries[0].Children) != 1 || groups[1].Type != "tomorrow" || !groups[1].Entries[0].IsDeadline {
		t.Fatalf("wrong groups: %+v", groups)
	}
}
