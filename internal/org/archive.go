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

var closedRe = regexp.MustCompile(`CLOSED:\s*\[([^\]]+)\]`)

type ArchiveCandidate struct {
	File      string
	Line      int
	State     string
	Title     string
	Tags      []string
	ClosedStr string
}

type ArchiveRef struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

func FindArchivable(dir string) ([]ArchiveCandidate, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.org"))
	if err != nil {
		return nil, err
	}
	var result []ArchiveCandidate
	for _, f := range files {
		base := filepath.Base(f)
		if base == "archive.org" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		active, done := fileTodoDef(lines)
		allStates := makeSet(append(active, done...))
		doneMap := makeSet(done)

		for i := 0; i < len(lines); i++ {
			m := headlineRe.FindStringSubmatch(lines[i])
			if m == nil || len(m[1]) != 1 {
				continue
			}
			rest := m[2]
			var state string
			if idx := strings.IndexByte(rest, ' '); idx > 0 {
				w := rest[:idx]
				if allStates[w] {
					if doneMap[w] {
						state = w
					}
					rest = rest[idx+1:]
				}
			} else if doneMap[rest] {
				state = rest
				rest = ""
			}
			if state == "" {
				continue
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

			closedStr := ""
			for j := i + 1; j < len(lines) && j < i+5; j++ {
				if cm := closedRe.FindStringSubmatch(lines[j]); cm != nil {
					closedStr = cm[1]
					break
				}
				if headlineRe.MatchString(lines[j]) {
					break
				}
			}

			result = append(result, ArchiveCandidate{
				File:      base,
				Line:      i + 1,
				State:     state,
				Title:     strings.TrimSpace(rest),
				Tags:      tags,
				ClosedStr: closedStr,
			})
		}
	}
	return result, nil
}

func Archive(dir string, refs []ArchiveRef, now time.Time) error {
	byFile := map[string][]int{}
	for _, r := range refs {
		if strings.Contains(r.File, "/") || strings.Contains(r.File, "..") {
			return fmt.Errorf("invalid file: %s", r.File)
		}
		byFile[r.File] = append(byFile[r.File], r.Line)
	}

	archivePath := filepath.Join(dir, "archive.org")
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		if err := os.WriteFile(archivePath, []byte("#+TODO: TODO WAIT | DONE CANCELED\n"), 0644); err != nil {
			return err
		}
	}

	var archiveBuf strings.Builder

	for file, lineNums := range byFile {
		path := filepath.Join(dir, file)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		lines := strings.Split(string(data), "\n")
		sort.Sort(sort.Reverse(sort.IntSlice(lineNums)))

		for _, ln := range lineNums {
			idx := ln - 1
			if idx < 0 || idx >= len(lines) {
				continue
			}
			m := headlineRe.FindStringSubmatch(lines[idx])
			if m == nil || len(m[1]) != 1 {
				continue
			}

			endIdx := len(lines)
			for j := idx + 1; j < len(lines); j++ {
				if m2 := headlineRe.FindStringSubmatch(lines[j]); m2 != nil && len(m2[1]) == 1 {
					endIdx = j
					break
				}
			}
			for endIdx > idx+1 && strings.TrimSpace(lines[endIdx-1]) == "" {
				endIdx--
			}

			entryLines := make([]string, endIdx-idx)
			copy(entryLines, lines[idx:endIdx])
			entryLines = addArchiveProps(entryLines, filepath.Join(dir, file), now)

			archiveBuf.WriteString("\n")
			archiveBuf.WriteString(strings.Join(entryLines, "\n"))
			archiveBuf.WriteString("\n")

			lines = append(lines[:idx], lines[endIdx:]...)
		}

		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", file, err)
		}
	}

	f, err := os.OpenFile(archivePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	defer f.Close()
	_, err = f.WriteString(archiveBuf.String())
	return err
}

func addArchiveProps(lines []string, filePath string, now time.Time) []string {
	state := ""
	if m := headlineRe.FindStringSubmatch(lines[0]); m != nil {
		words := strings.SplitN(m[2], " ", 2)
		known := map[string]bool{"DONE": true, "CANCELED": true, "TODO": true, "WAIT": true}
		if len(words) > 0 && known[words[0]] {
			state = words[0]
		}
	}

	category := strings.TrimSuffix(filepath.Base(filePath), ".org")
	props := []string{
		":ARCHIVE_TIME: " + now.Format("2006-01-02 Mon 15:04"),
		":ARCHIVE_FILE: " + filePath,
		":ARCHIVE_CATEGORY: " + category,
	}
	if state != "" {
		props = append(props, ":ARCHIVE_TODO: "+state)
	}

	propStart, propEnd := -1, -1
	for i := 1; i < len(lines) && i < 10; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "*") {
			break
		}
		if trimmed == ":PROPERTIES:" {
			propStart = i
		}
		if trimmed == ":END:" && propStart >= 0 {
			propEnd = i
			break
		}
	}

	if propStart >= 0 && propEnd >= 0 {
		result := make([]string, 0, len(lines)+len(props))
		result = append(result, lines[:propEnd]...)
		result = append(result, props...)
		result = append(result, lines[propEnd:]...)
		return result
	}

	insertIdx := 1
	for i := 1; i < len(lines) && i < 4; i++ {
		l := lines[i]
		if strings.Contains(l, "CLOSED:") || strings.Contains(l, "SCHEDULED:") || strings.Contains(l, "DEADLINE:") {
			insertIdx = i + 1
			continue
		}
		break
	}

	block := append([]string{":PROPERTIES:"}, props...)
	block = append(block, ":END:")

	result := make([]string, 0, len(lines)+len(block))
	result = append(result, lines[:insertIdx]...)
	result = append(result, block...)
	result = append(result, lines[insertIdx:]...)
	return result
}
