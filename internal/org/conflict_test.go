package org

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testPreamble = "#+TODO: TODO WAIT | DONE CANCELED\n#+ARCHIVE: archive.org::\n"

func writeConflictPair(t *testing.T, base, other string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "work.org"), []byte(base), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "work.sync-conflict-20260701-235704-UQ77RGB.org"), []byte(other), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSyncConflictBase(t *testing.T) {
	if got := SyncConflictBase("work.sync-conflict-20260701-235704-UQ77RGB.org"); got != "work.org" {
		t.Errorf("got %q", got)
	}
	if got := SyncConflictBase("work.org"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestMergeDoneBeatsReschedule(t *testing.T) {
	base := testPreamble + "\n* DONE Task one :srk:\nCLOSED: [2026-07-01 Wed 23:35] SCHEDULED: <2026-07-01 Wed>\n\n* Untouched\nSCHEDULED: <2026-07-02 Thu>\n"
	other := testPreamble + "\n* Task one :srk:\nSCHEDULED: <2026-07-02 Thu>\n\n* Untouched\nSCHEDULED: <2026-07-02 Thu>\n"
	dir := writeConflictPair(t, base, other)

	text, pair, err := MergeConflict(dir, "work.org", "work.sync-conflict-20260701-235704-UQ77RGB.org", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pair.Items) != 0 {
		t.Fatalf("expected clean merge, got items: %+v", pair.Items)
	}
	if pair.AutoResolved != 1 {
		t.Errorf("AutoResolved = %d, want 1", pair.AutoResolved)
	}
	if !strings.Contains(text, "* DONE Task one :srk:") {
		t.Errorf("done version must win:\n%s", text)
	}
	if strings.Contains(text, "* Task one :srk:\nSCHEDULED: <2026-07-02 Thu>") {
		t.Errorf("rescheduled version must not survive:\n%s", text)
	}
}

func TestMergeDoneWinsAlsoWhenConflictSideIsDone(t *testing.T) {
	base := testPreamble + "\n* Task one\nSCHEDULED: <2026-07-02 Thu>\n"
	other := testPreamble + "\n* CANCELED Task one\nCLOSED: [2026-07-01 Wed 10:00] SCHEDULED: <2026-07-01 Wed>\n"
	dir := writeConflictPair(t, base, other)

	text, pair, err := MergeConflict(dir, "work.org", "work.sync-conflict-20260701-235704-UQ77RGB.org", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pair.Items) != 0 {
		t.Fatalf("expected clean merge, got items: %+v", pair.Items)
	}
	if !strings.Contains(text, "* CANCELED Task one") {
		t.Errorf("done (CANCELED) version must win:\n%s", text)
	}
}

func TestMergeUnionKeepsBothSidesAdditions(t *testing.T) {
	base := testPreamble + "\n* Shared\nSCHEDULED: <2026-07-02 Thu>\n\n* Only in base\n"
	other := testPreamble + "\n* Added before shared\n\n* Shared\nSCHEDULED: <2026-07-02 Thu>\n\n* Only in conflict\nSCHEDULED: <2026-07-03 Fri>\n"
	dir := writeConflictPair(t, base, other)

	text, pair, err := MergeConflict(dir, "work.org", "work.sync-conflict-20260701-235704-UQ77RGB.org", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pair.Items) != 0 {
		t.Fatalf("expected clean merge, got items: %+v", pair.Items)
	}
	if pair.Added != 2 {
		t.Errorf("Added = %d, want 2", pair.Added)
	}
	for _, want := range []string{"* Only in base", "* Only in conflict", "* Added before shared", "* Shared"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "* Added before shared") > strings.Index(text, "* Shared") {
		t.Errorf("conflict-only entry lost its position:\n%s", text)
	}
}

func TestMergeBodyDiffNeedsManualChoice(t *testing.T) {
	base := testPreamble + "\n* Notes\nSCHEDULED: <2026-07-02 Thu>\n\nbase body text\n"
	other := testPreamble + "\n* Notes\nSCHEDULED: <2026-07-02 Thu>\n\nconflict body text\n"
	dir := writeConflictPair(t, base, other)

	_, pair, err := MergeConflict(dir, "work.org", "work.sync-conflict-20260701-235704-UQ77RGB.org", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pair.Items) != 1 {
		t.Fatalf("expected 1 manual item, got %+v", pair.Items)
	}

	// choosing the conflict version resolves it and removes the copy
	err = ResolveConflict(dir, "work.org", "work.sync-conflict-20260701-235704-UQ77RGB.org",
		map[string]string{pair.Items[0].Key: "other"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "work.org"))
	if !strings.Contains(string(data), "conflict body text") {
		t.Errorf("chosen version not applied:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "work.sync-conflict-20260701-235704-UQ77RGB.org")); !os.IsNotExist(err) {
		t.Error("conflict file should be deleted after resolve")
	}
}

func TestMergeLastRepeatDiffIsAuto(t *testing.T) {
	base := testPreamble + "\n* Recurring\nSCHEDULED: <2026-07-08 Wed +1w>\n:PROPERTIES:\n:LAST_REPEAT: [2026-07-01 Wed 20:00]\n:END:\n"
	other := testPreamble + "\n* Recurring\nSCHEDULED: <2026-07-01 Wed +1w>\n:PROPERTIES:\n:LAST_REPEAT: [2026-06-24 Wed 20:00]\n:END:\n"
	dir := writeConflictPair(t, base, other)

	text, pair, err := MergeConflict(dir, "work.org", "work.sync-conflict-20260701-235704-UQ77RGB.org", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pair.Items) != 0 {
		t.Fatalf("expected clean merge, got items: %+v", pair.Items)
	}
	if !strings.Contains(text, "<2026-07-08 Wed +1w>") {
		t.Errorf("base (advanced) schedule must win:\n%s", text)
	}
}

func TestAutoMergeConflictsWritesAndRemoves(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	base := testPreamble + "\n* DONE Task one\nCLOSED: [2026-07-01 Wed 23:35] SCHEDULED: <2026-07-01 Wed>\n"
	other := testPreamble + "\n* Task one\nSCHEDULED: <2026-07-02 Thu>\n\n* New from phone\nSCHEDULED: <2026-07-02 Thu>\n"
	dir := writeConflictPair(t, base, other)

	merged, remaining, err := AutoMergeConflicts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 1 || len(remaining) != 0 {
		t.Fatalf("merged=%d remaining=%d", len(merged), len(remaining))
	}
	if _, err := os.Stat(filepath.Join(dir, "work.sync-conflict-20260701-235704-UQ77RGB.org")); !os.IsNotExist(err) {
		t.Error("conflict file should be removed")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "work.org"))
	if !strings.Contains(string(data), "* DONE Task one") || !strings.Contains(string(data), "* New from phone") {
		t.Errorf("merged content wrong:\n%s", data)
	}

	// parse result to make sure the merged file is still valid org
	entries, err := ParseFile(filepath.Join(dir, "work.org"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("merged file must parse: %v, %d entries", err, len(entries))
	}
}

func TestAutoMergeLeavesManualConflictsAlone(t *testing.T) {
	base := testPreamble + "\n* Notes\n\nbase body\n"
	other := testPreamble + "\n* Notes\n\nconflict body\n"
	dir := writeConflictPair(t, base, other)

	merged, remaining, err := AutoMergeConflicts(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 0 || len(remaining) != 1 {
		t.Fatalf("merged=%d remaining=%d", len(merged), len(remaining))
	}
	// nothing touched
	data, _ := os.ReadFile(filepath.Join(dir, "work.org"))
	if string(data) != base {
		t.Error("base file must not be modified when manual conflicts remain")
	}
	if _, err := os.Stat(filepath.Join(dir, "work.sync-conflict-20260701-235704-UQ77RGB.org")); err != nil {
		t.Error("conflict file must remain")
	}
}

func TestDeleteConflictFileValidates(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := writeConflictPair(t, "x\n", "y\n")
	if err := DeleteConflictFile(dir, "work.org"); err == nil {
		t.Error("deleting a non-conflict file must be rejected")
	}
	if err := DeleteConflictFile(dir, "../work.sync-conflict-20260701-235704-UQ77RGB.org"); err == nil {
		t.Error("path traversal must be rejected")
	}
	if err := DeleteConflictFile(dir, "work.sync-conflict-20260701-235704-UQ77RGB.org"); err != nil {
		t.Errorf("valid delete failed: %v", err)
	}
}

func TestParseDirSkipsConflictFiles(t *testing.T) {
	dir := writeConflictPair(t,
		testPreamble+"\n* Task\nSCHEDULED: <2026-07-02 Thu>\n",
		testPreamble+"\n* Task\nSCHEDULED: <2026-07-02 Thu>\n")
	entries, err := ParseDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("conflict file must be excluded from parsing, got %d entries", len(entries))
	}
}
