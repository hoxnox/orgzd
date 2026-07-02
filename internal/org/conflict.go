package org

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Syncthing renames the losing copy of a conflicting file to
// name.sync-conflict-YYYYMMDD-HHMMSS-DEVICE.org and keeps the winner
// under the original name.
var syncConflictRe = regexp.MustCompile(`^(.+)\.sync-conflict-\d{8}-\d{6}-[A-Za-z0-9]+\.org$`)

func IsSyncConflict(name string) bool {
	return syncConflictRe.MatchString(name)
}

// SyncConflictBase returns the original file name for a conflict file
// name, or "" if the name is not a sync-conflict name.
func SyncConflictBase(name string) string {
	m := syncConflictRe.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	return m[1] + ".org"
}

// ConflictItem is one entry that differs between the two files in a way
// that cannot be resolved automatically (the body text differs).
type ConflictItem struct {
	Key       string
	Title     string
	BaseText  string
	OtherText string
}

// ConflictPair describes a base file and one of its sync-conflict copies.
type ConflictPair struct {
	Base         string
	Conflict     string
	Items        []ConflictItem
	AutoResolved int // entries that differed but were merged by rule
	Added        int // entries that existed only in the conflict copy
	Err          string
}

// FindConflictFiles returns conflict file names grouped by their base
// file name, both sorted for stable output.
func FindConflictFiles(dir string) (map[string][]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.org"))
	if err != nil {
		return nil, err
	}
	byBase := map[string][]string{}
	for _, f := range files {
		name := filepath.Base(f)
		if base := SyncConflictBase(name); base != "" {
			byBase[base] = append(byBase[base], name)
		}
	}
	for _, cs := range byBase {
		sort.Strings(cs)
	}
	return byBase, nil
}

const preambleKey = "__preamble__"

type mergeBlock struct {
	key    string
	raw    []string // exact source lines, incl. trailing blank lines
	isDone bool
}

// trimmed returns the block content without trailing blank lines, for
// comparisons and for substituting a block into the other file's layout.
func (b *mergeBlock) trimmed() []string {
	return trimTrailingBlank(b.raw)
}

type splitFileResult struct {
	preamble []string // exact source lines before the first headline
	blocks   []mergeBlock
	byKey    map[string]*mergeBlock
}

func splitFile(lines []string) *splitFileResult {
	active, done := fileTodoDef(lines)
	allStates := makeSet(append(active, done...))
	doneMap := makeSet(done)

	r := &splitFileResult{byKey: map[string]*mergeBlock{}}
	seen := map[string]int{}
	var cur *mergeBlock
	flush := func(raw []string) {
		if cur == nil {
			r.preamble = raw
			return
		}
		cur.raw = raw
		r.blocks = append(r.blocks, *cur)
	}

	start := 0
	for i, line := range lines {
		m := headlineRe.FindStringSubmatch(line)
		if m == nil || len(m[1]) != 1 {
			continue
		}
		flush(lines[start:i])
		start = i

		rest := m[2]
		var state string
		if idx := strings.IndexByte(rest, ' '); idx > 0 {
			if allStates[rest[:idx]] {
				state = rest[:idx]
				rest = rest[idx+1:]
			}
		} else if allStates[rest] {
			state = rest
			rest = ""
		}
		if tm := tagsRe.FindStringSubmatch(rest); tm != nil {
			rest = rest[:len(rest)-len(tm[0])] + " :" + tm[1] + ":"
		}
		key := strings.TrimSpace(rest)
		seen[key]++
		if n := seen[key]; n > 1 {
			key = fmt.Sprintf("%s#%d", key, n)
		}
		cur = &mergeBlock{key: key, isDone: doneMap[state]}
	}
	flush(lines[start:])
	for i := range r.blocks {
		r.byKey[r.blocks[i].key] = &r.blocks[i]
	}
	return r
}

func trimTrailingBlank(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// blockBodyKey extracts the entry content that must match for a
// difference to be auto-resolvable: everything except the headline
// itself, the planning line and :LAST_REPEAT: (all of which legitimately
// change when a task is completed or rescheduled on one device).
func blockBodyKey(lines []string) string {
	var out []string
	for i, l := range lines {
		if i == 0 {
			continue
		}
		if i == 1 && (strings.Contains(l, "SCHEDULED:") || strings.Contains(l, "DEADLINE:") || strings.Contains(l, "CLOSED:")) {
			continue
		}
		if strings.Contains(l, ":LAST_REPEAT:") {
			continue
		}
		out = append(out, strings.TrimRight(l, " \t"))
	}
	return strings.Join(trimTrailingBlank(out), "\n")
}

// autoPick resolves a differing pair when only state/planning changed:
// a completed version beats an uncompleted one (marking done must never
// be undone by a merge), otherwise the base file wins — Syncthing keeps
// the newer copy under the base name.
func autoPick(base, other *mergeBlock) ([]string, bool) {
	if blockBodyKey(base.raw) != blockBodyKey(other.raw) {
		return nil, false
	}
	if other.isDone && !base.isDone {
		return other.trimmed(), true
	}
	return base.trimmed(), true
}

// MergeConflict computes the merge of dir/base with dir/conflict.
// Entries present in only one file are kept (union). Entries that
// differ only in state/planning are resolved automatically. Entries
// whose body differs become ConflictItems; choices ("base"/"other" per
// item key) settles them, unsettled items default to the base version
// in the returned text but stay listed in pair.Items.
func MergeConflict(dir, base, conflict string, choices map[string]string) (string, ConflictPair, error) {
	pair := ConflictPair{Base: base, Conflict: conflict}
	if err := validateConflictNames(base, conflict); err != nil {
		return "", pair, err
	}
	otherData, err := os.ReadFile(filepath.Join(dir, conflict))
	if err != nil {
		return "", pair, err
	}
	baseData, err := os.ReadFile(filepath.Join(dir, base))
	if os.IsNotExist(err) {
		// base was deleted on this device; keep the conflict copy's data
		return string(otherData), pair, nil
	}
	if err != nil {
		return "", pair, err
	}

	bf := splitFile(strings.Split(string(baseData), "\n"))
	of := splitFile(strings.Split(string(otherData), "\n"))

	// pick returns the trimmed version chosen for a manual conflict,
	// defaulting to base and recording the item when no choice is given
	pick := func(key string, baseLines, otherLines []string, title string) []string {
		switch choices[key] {
		case "base":
			return baseLines
		case "other":
			return otherLines
		}
		pair.Items = append(pair.Items, ConflictItem{
			Key:       key,
			Title:     title,
			BaseText:  strings.Join(baseLines, "\n"),
			OtherText: strings.Join(otherLines, "\n"),
		})
		return baseLines
	}

	pre := bf.preamble
	bpre, opre := trimTrailingBlank(bf.preamble), trimTrailingBlank(of.preamble)
	if !equalLines(bpre, opre) {
		switch {
		case len(bpre) == 0:
			pre = of.preamble
		case len(opre) == 0:
			// keep base
		default:
			if chosen := pick(preambleKey, bpre, opre, "(file header)"); equalLines(chosen, opre) {
				pre = of.preamble
			}
		}
	}

	// conflict-only blocks are inserted after the nearest preceding
	// block that both files share, preserving their relative position
	anchored := map[string][]*mergeBlock{}
	lastShared := ""
	for i := range of.blocks {
		ob := &of.blocks[i]
		if _, ok := bf.byKey[ob.key]; ok {
			lastShared = ob.key
		} else {
			anchored[lastShared] = append(anchored[lastShared], ob)
		}
	}

	out := append([]string{}, pre...)
	emitAnchored := func(key string) {
		for _, ob := range anchored[key] {
			out = append(out, ob.raw...)
			pair.Added++
		}
	}
	// substitute content into a base block's slot, keeping the base
	// file's trailing blank lines so untouched layout stays byte-exact
	emitInBaseSlot := func(bb *mergeBlock, content []string) {
		out = append(out, content...)
		out = append(out, bb.raw[len(bb.trimmed()):]...)
	}
	emitAnchored("")
	for i := range bf.blocks {
		bb := &bf.blocks[i]
		ob := of.byKey[bb.key]
		switch {
		case ob == nil || equalLines(bb.trimmed(), ob.trimmed()):
			out = append(out, bb.raw...)
		default:
			if chosen, ok := autoPick(bb, ob); ok {
				emitInBaseSlot(bb, chosen)
				pair.AutoResolved++
			} else {
				emitInBaseSlot(bb, pick(bb.key, bb.trimmed(), ob.trimmed(), bb.key))
			}
		}
		if ob != nil {
			emitAnchored(bb.key)
		}
	}

	text := strings.Join(out, "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text, pair, nil
}

// AutoMergeConflicts merges every conflict file that resolves cleanly
// (writes the base file, backs up and removes the conflict copy) and
// returns the pairs that still need manual resolution.
func AutoMergeConflicts(dir string) (merged, remaining []ConflictPair, err error) {
	byBase, err := FindConflictFiles(dir)
	if err != nil {
		return nil, nil, err
	}
	var bases []string
	for b := range byBase {
		bases = append(bases, b)
	}
	sort.Strings(bases)
	for _, base := range bases {
		for _, conflict := range byBase[base] {
			text, pair, err := MergeConflict(dir, base, conflict, nil)
			if err != nil {
				pair.Err = err.Error()
				remaining = append(remaining, pair)
				continue
			}
			if len(pair.Items) > 0 {
				remaining = append(remaining, pair)
				continue
			}
			if err := finalizeMerge(dir, base, conflict, text); err != nil {
				pair.Err = err.Error()
				remaining = append(remaining, pair)
				continue
			}
			merged = append(merged, pair)
		}
	}
	return merged, remaining, nil
}

// ResolveConflict applies the user's per-entry choices and finalizes
// the merge. Every ConflictItem must have a choice.
func ResolveConflict(dir, base, conflict string, choices map[string]string) error {
	text, pair, err := MergeConflict(dir, base, conflict, choices)
	if err != nil {
		return err
	}
	if len(pair.Items) > 0 {
		return fmt.Errorf("unresolved entries remain (files changed? reload the page): %q", pair.Items[0].Key)
	}
	return finalizeMerge(dir, base, conflict, text)
}

// DeleteConflictFile discards a conflict copy, keeping the base file
// as-is. The copy is backed up first.
func DeleteConflictFile(dir, conflict string) error {
	if !IsSyncConflict(conflict) || strings.Contains(conflict, "/") || strings.Contains(conflict, "..") {
		return fmt.Errorf("not a sync-conflict file: %q", conflict)
	}
	if err := backupFiles(dir, conflict); err != nil {
		return err
	}
	return os.Remove(filepath.Join(dir, conflict))
}

func finalizeMerge(dir, base, conflict, text string) error {
	if err := backupFiles(dir, base, conflict); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, base), []byte(text), 0644); err != nil {
		return err
	}
	return os.Remove(filepath.Join(dir, conflict))
}

func validateConflictNames(base, conflict string) error {
	for _, n := range []string{base, conflict} {
		if n == "" || strings.Contains(n, "/") || strings.Contains(n, "..") {
			return fmt.Errorf("invalid file name: %q", n)
		}
	}
	if SyncConflictBase(conflict) != base {
		return fmt.Errorf("%q is not a sync-conflict copy of %q", conflict, base)
	}
	return nil
}

// backupFiles copies the named files into the user cache dir so no
// merge or delete can lose data irrecoverably.
func backupFiles(dir string, names ...string) error {
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	bdir := filepath.Join(cache, "orgzd", "conflict-backups", time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(bdir, 0755); err != nil {
		return err
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(bdir, name), data, 0644); err != nil {
			return err
		}
	}
	return nil
}
