package org

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var checkboxRe = regexp.MustCompile(`^(\s*)([-+]|\d+[.)])(\s+)\[([ xX-])\](.*)$`)

// ToggleCheckbox sets the state of the index-th checkbox list item
// ("- [ ] ...", also +/numbered bullets) in the body of the entry whose
// headline is at line. Drawer content is skipped, mirroring what the
// parser exposes as Body, so indexes match what the UI renders.
func ToggleCheckbox(dir, file string, line, index int, checked bool) error {
	if strings.Contains(file, "/") || strings.Contains(file, "..") {
		return fmt.Errorf("invalid file")
	}
	path := filepath.Join(dir, file)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	idx := line - 1
	if idx < 0 || idx >= len(lines) || !headlineRe.MatchString(lines[idx]) {
		return fmt.Errorf("line %d is not a headline", line)
	}

	n := 0
	inDrawer := false
	for j := idx + 1; j < len(lines); j++ {
		if headlineRe.MatchString(lines[j]) {
			break
		}
		if inDrawer {
			if strings.TrimSpace(lines[j]) == ":END:" {
				inDrawer = false
			}
			continue
		}
		if drawerOpenRe.MatchString(lines[j]) {
			inDrawer = true
			continue
		}
		m := checkboxRe.FindStringSubmatch(lines[j])
		if m == nil {
			continue
		}
		if n == index {
			mark := " "
			if checked {
				mark = "x"
			}
			lines[j] = m[1] + m[2] + m[3] + "[" + mark + "]" + m[5]
			return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
		}
		n++
	}
	return fmt.Errorf("checkbox %d not found", index)
}
