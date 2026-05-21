package org

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type EntryDraft struct {
	File      string   `json:"file"`
	Line      int      `json:"line"`
	State     string   `json:"state"`
	Title     string   `json:"title"`
	Tags      []string `json:"tags"`
	Scheduled string   `json:"scheduled"`
	Deadline  string   `json:"deadline"`
	Body      string   `json:"body"`
}

func ListFiles(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.org"))
	if err != nil {
		return nil, err
	}
	var files []string
	for _, m := range matches {
		base := filepath.Base(m)
		if base == "archive.org" {
			continue
		}
		files = append(files, base)
	}
	sort.Strings(files)
	return files, nil
}

func LoadEntry(dir, file string, line int) (*EntryDraft, error) {
	if strings.Contains(file, "/") || strings.Contains(file, "..") {
		return nil, fmt.Errorf("invalid file")
	}
	data, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	idx := line - 1
	if idx < 0 || idx >= len(lines) {
		return nil, fmt.Errorf("line out of range")
	}
	m := headlineRe.FindStringSubmatch(lines[idx])
	if m == nil {
		return nil, fmt.Errorf("not a headline")
	}

	active, done := fileTodoDef(lines)
	allStates := makeSet(append(active, done...))

	rest := m[2]
	var state string
	if i := strings.IndexByte(rest, ' '); i > 0 && allStates[rest[:i]] {
		state = rest[:i]
		rest = rest[i+1:]
	} else if allStates[rest] {
		state = rest
		rest = ""
	}

	var tags []string
	if tm := tagsRe.FindStringSubmatch(rest); tm != nil {
		for _, t := range strings.Split(tm[1], ":") {
			if t != "" {
				tags = append(tags, t)
			}
		}
		rest = rest[:len(rest)-len(tm[0])]
	}

	title := strings.TrimSpace(rest)

	scheduledStr := ""
	deadlineStr := ""
	i := idx + 1
	if i < len(lines) {
		l := lines[i]
		if strings.Contains(l, "SCHEDULED:") || strings.Contains(l, "DEADLINE:") || strings.Contains(l, "CLOSED:") {
			if strings.Contains(l, "SCHEDULED:") {
				if ts := parseTimestamp(l[strings.Index(l, "SCHEDULED:"):]); ts != nil {
					scheduledStr = timestampToInput(ts)
				}
			}
			if strings.Contains(l, "DEADLINE:") {
				if ts := parseTimestamp(l[strings.Index(l, "DEADLINE:"):]); ts != nil {
					deadlineStr = timestampToInput(ts)
				}
			}
			i++
		}
	}

	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			i++
			continue
		}
		if drawerOpenRe.MatchString(lines[i]) {
			for i < len(lines) && strings.TrimSpace(lines[i]) != ":END:" {
				i++
			}
			if i < len(lines) {
				i++
			}
		} else {
			break
		}
	}

	bodyStart := i
	bodyEnd := i
	for bodyEnd < len(lines) {
		if headlineRe.MatchString(lines[bodyEnd]) {
			break
		}
		bodyEnd++
	}
	for bodyEnd > bodyStart && strings.TrimSpace(lines[bodyEnd-1]) == "" {
		bodyEnd--
	}

	body := ""
	if bodyEnd > bodyStart {
		body = strings.Join(lines[bodyStart:bodyEnd], "\n")
	}

	return &EntryDraft{
		File:      file,
		Line:      line,
		State:     state,
		Title:     title,
		Tags:      tags,
		Scheduled: scheduledStr,
		Deadline:  deadlineStr,
		Body:      body,
	}, nil
}

func CreateEntry(dir string, d *EntryDraft) error {
	if err := validateDraft(d, false); err != nil {
		return err
	}
	path := filepath.Join(dir, d.File)

	var out []string
	out = append(out, "")
	out = append(out, formatHeadline("*", d.State, d.Title, d.Tags))
	if p := formatPlanning(d.Scheduled, d.Deadline, ""); p != "" {
		out = append(out, p)
	}
	if d.Body != "" {
		out = append(out, "")
		out = append(out, strings.Split(d.Body, "\n")...)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(strings.Join(out, "\n") + "\n")
	return err
}

func UpdateEntry(dir string, d *EntryDraft) error {
	if err := validateDraft(d, true); err != nil {
		return err
	}
	path := filepath.Join(dir, d.File)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	idx := d.Line - 1
	if idx < 0 || idx >= len(lines) {
		return fmt.Errorf("line out of range")
	}
	m := headlineRe.FindStringSubmatch(lines[idx])
	if m == nil {
		return fmt.Errorf("not a headline at line %d", d.Line)
	}
	stars := m[1]
	level := len(stars)

	i := idx + 1
	var closedStr string
	if i < len(lines) {
		l := lines[i]
		if strings.Contains(l, "SCHEDULED:") || strings.Contains(l, "DEADLINE:") || strings.Contains(l, "CLOSED:") {
			if cm := closedRe.FindStringSubmatch(l); cm != nil {
				closedStr = "[" + cm[1] + "]"
			}
			i++
		}
	}

	drawerStart := i
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			i++
			continue
		}
		if drawerOpenRe.MatchString(lines[i]) {
			for i < len(lines) && strings.TrimSpace(lines[i]) != ":END:" {
				i++
			}
			if i < len(lines) {
				i++
			}
		} else {
			break
		}
	}
	drawerEnd := i

	bodyEnd := i
	for bodyEnd < len(lines) {
		if headlineRe.MatchString(lines[bodyEnd]) {
			break
		}
		bodyEnd++
	}

	entryEnd := bodyEnd
	for entryEnd < len(lines) {
		if hm := headlineRe.FindStringSubmatch(lines[entryEnd]); hm != nil {
			if len(hm[1]) <= level {
				break
			}
		}
		entryEnd++
	}

	var out []string
	out = append(out, formatHeadline(stars, d.State, d.Title, d.Tags))
	if p := formatPlanning(d.Scheduled, d.Deadline, closedStr); p != "" {
		out = append(out, p)
	}

	drawerLines := lines[drawerStart:drawerEnd]
	for len(drawerLines) > 0 && strings.TrimSpace(drawerLines[0]) == "" {
		drawerLines = drawerLines[1:]
	}
	for len(drawerLines) > 0 && strings.TrimSpace(drawerLines[len(drawerLines)-1]) == "" {
		drawerLines = drawerLines[:len(drawerLines)-1]
	}
	if len(drawerLines) > 0 {
		out = append(out, drawerLines...)
	}

	if d.Body != "" {
		out = append(out, "")
		out = append(out, strings.Split(d.Body, "\n")...)
	}

	if bodyEnd < entryEnd {
		out = append(out, "")
		out = append(out, lines[bodyEnd:entryEnd]...)
	}

	if entryEnd > idx && strings.TrimSpace(lines[entryEnd-1]) == "" {
		out = append(out, "")
	}

	result := append([]string{}, lines[:idx]...)
	result = append(result, out...)
	result = append(result, lines[entryEnd:]...)

	return os.WriteFile(path, []byte(strings.Join(result, "\n")), 0644)
}

func validateDraft(d *EntryDraft, requireLine bool) error {
	if d.File == "" {
		return fmt.Errorf("file required")
	}
	if strings.Contains(d.File, "/") || strings.Contains(d.File, "..") {
		return fmt.Errorf("invalid file")
	}
	if strings.TrimSpace(d.Title) == "" {
		return fmt.Errorf("title required")
	}
	if requireLine && d.Line < 1 {
		return fmt.Errorf("line required")
	}
	return nil
}

func formatHeadline(stars, state, title string, tags []string) string {
	h := stars
	if state != "" {
		h += " " + state
	}
	h += " " + strings.TrimSpace(title)
	if len(tags) > 0 {
		h += " :" + strings.Join(tags, ":") + ":"
	}
	return h
}

func formatPlanning(scheduledStr, deadlineStr, closedStr string) string {
	var parts []string
	if closedStr != "" {
		parts = append(parts, "CLOSED: "+closedStr)
	}
	if scheduledStr != "" {
		parts = append(parts, "SCHEDULED: <"+normalizeOrgTimestamp(scheduledStr)+">")
	}
	if deadlineStr != "" {
		parts = append(parts, "DEADLINE: <"+normalizeOrgTimestamp(deadlineStr)+">")
	}
	return strings.Join(parts, " ")
}

func normalizeOrgTimestamp(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<")
	s = strings.TrimSuffix(s, ">")
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return ""
	}
	t, err := time.ParseInLocation("2006-01-02", parts[0], time.Local)
	if err != nil {
		return s
	}
	result := t.Format("2006-01-02 Mon")
	for _, p := range parts[1:] {
		if len(p) == 3 && isAlphaASCII(p) {
			continue
		}
		result += " " + p
	}
	return result
}

func timestampToInput(ts *Timestamp) string {
	s := ts.Time.Format("2006-01-02")
	if ts.HasTime {
		s += " " + ts.Time.Format("15:04")
	}
	if ts.Repeater != "" {
		s += " " + ts.Repeater
	}
	return s
}

func isAlphaASCII(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}
