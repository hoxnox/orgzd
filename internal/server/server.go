package server

import (
	"embed"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"orgzd/internal/agenda"
	"orgzd/internal/org"
)

//go:embed agenda.html maintenance.html tag.html edit.html conflicts.html
var tmplFS embed.FS

var (
	agendaTmpl    = template.Must(template.ParseFS(tmplFS, "agenda.html"))
	maintTmpl     = template.Must(template.ParseFS(tmplFS, "maintenance.html"))
	tagTmpl       = template.Must(template.ParseFS(tmplFS, "tag.html"))
	editTmpl      = template.Must(template.ParseFS(tmplFS, "edit.html"))
	conflictsTmpl = template.Must(template.ParseFS(tmplFS, "conflicts.html"))
)

// mergeMu serializes conflict auto-merging so concurrent page loads
// don't merge the same files twice.
var mergeMu sync.Mutex

func autoMergeConflicts(dir string) []org.ConflictPair {
	mergeMu.Lock()
	defer mergeMu.Unlock()
	merged, remaining, err := org.AutoMergeConflicts(dir)
	if err != nil {
		log.Printf("conflict scan: %v", err)
		return nil
	}
	for _, p := range merged {
		log.Printf("auto-merged %s into %s (resolved %d, added %d)", p.Conflict, p.Base, p.AutoResolved, p.Added)
	}
	return remaining
}

func parseTagsInput(s string) []string {
	var tags []string
	for _, t := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == ':'
	}) {
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

type editFormData struct {
	DateStr string
	Mode    string
	Files   []string
	States  []string
	Draft   *org.EntryDraft
	TagsStr string
}

func Start(addr, dir string) error {
	http.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		conflicts := autoMergeConflicts(dir)
		entries, err := org.ParseDir(dir)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		groups := agenda.Build(entries, time.Now())
		var inbox []*org.Entry
		for _, e := range entries {
			if e.File == "inbox.org" && !e.IsDone && e.Scheduled == nil && e.Deadline == nil {
				inbox = append(inbox, e)
			}
		}
		data := struct {
			DateStr       string
			Groups        []agenda.Group
			Inbox         []*org.Entry
			ConflictCount int
		}{
			DateStr:       time.Now().Format("Monday, 02 January 2006"),
			Groups:        groups,
			Inbox:         inbox,
			ConflictCount: len(conflicts),
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := agendaTmpl.Execute(w, data); err != nil {
			log.Printf("template: %v", err)
		}
	})

	http.HandleFunc("POST /api/reschedule-overdue", func(w http.ResponseWriter, r *http.Request) {
		entries, err := org.ParseDir(dir)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		now := time.Now()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		var refs []org.ScheduleRef
		for _, e := range entries {
			if e.IsDone || e.Scheduled == nil {
				continue
			}
			// recurring tasks keep their own rhythm — a mass move would
			// shift the repeater's anchor date
			if e.Scheduled.Repeater != "" {
				continue
			}
			d := time.Date(e.Scheduled.Time.Year(), e.Scheduled.Time.Month(), e.Scheduled.Time.Day(), 0, 0, 0, 0, now.Location())
			if !d.Before(today) {
				continue
			}
			refs = append(refs, org.ScheduleRef{File: e.File, Line: e.Line})
		}
		if err := org.RescheduleAll(dir, refs, today); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(200)
	})

	http.HandleFunc("POST /api/schedule", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Entries []org.MoveScheduleRef `json:"entries"`
			When    string                `json:"when"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if len(req.Entries) == 0 {
			http.Error(w, "no entries", 400)
			return
		}
		now := time.Now()
		date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		switch req.When {
		case "today":
		case "tomorrow":
			date = date.AddDate(0, 0, 1)
		default:
			http.Error(w, "when must be today or tomorrow", 400)
			return
		}
		if err := org.ScheduleMove(dir, req.Entries, date); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(200)
	})

	http.HandleFunc("POST /api/done", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			File string `json:"file"`
			Line int    `json:"line"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if strings.Contains(req.File, "/") || strings.Contains(req.File, "..") {
			http.Error(w, "invalid file", 400)
			return
		}
		if err := org.MarkDone(dir, req.File, req.Line, time.Now()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(200)
	})

	http.HandleFunc("POST /api/checkbox", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			File    string `json:"file"`
			Line    int    `json:"line"`
			Index   int    `json:"index"`
			Checked bool   `json:"checked"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if strings.Contains(req.File, "/") || strings.Contains(req.File, "..") {
			http.Error(w, "invalid file", 400)
			return
		}
		if err := org.ToggleCheckbox(dir, req.File, req.Line, req.Index, req.Checked); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(200)
	})

	type fileGroup struct {
		Name    string
		Entries []org.ArchiveCandidate
	}

	http.HandleFunc("GET /maintenance", func(w http.ResponseWriter, r *http.Request) {
		candidates, err := org.FindArchivable(dir)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		gmap := map[string]*fileGroup{}
		var order []string
		for _, c := range candidates {
			if _, ok := gmap[c.File]; !ok {
				gmap[c.File] = &fileGroup{Name: c.File}
				order = append(order, c.File)
			}
			g := gmap[c.File]
			g.Entries = append(g.Entries, c)
		}
		sort.Strings(order)
		var files []fileGroup
		for _, name := range order {
			files = append(files, *gmap[name])
		}

		data := struct {
			DateStr string
			Files   []fileGroup
			Total   int
		}{
			DateStr: time.Now().Format("Monday, 02 January 2006"),
			Files:   files,
			Total:   len(candidates),
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := maintTmpl.Execute(w, data); err != nil {
			log.Printf("template: %v", err)
		}
	})

	http.HandleFunc("POST /api/archive", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Entries []org.ArchiveRef `json:"entries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if len(req.Entries) == 0 {
			http.Error(w, "no entries", 400)
			return
		}
		if err := org.Archive(dir, req.Entries, time.Now()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(200)
	})

	http.HandleFunc("GET /conflicts", func(w http.ResponseWriter, r *http.Request) {
		conflicts := autoMergeConflicts(dir)
		data := struct {
			DateStr string
			Pairs   []org.ConflictPair
		}{
			DateStr: time.Now().Format("Monday, 02 January 2006"),
			Pairs:   conflicts,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := conflictsTmpl.Execute(w, data); err != nil {
			log.Printf("template: %v", err)
		}
	})

	http.HandleFunc("POST /api/conflicts/resolve", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Base     string            `json:"base"`
			Conflict string            `json:"conflict"`
			Choices  map[string]string `json:"choices"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		mergeMu.Lock()
		err := org.ResolveConflict(dir, req.Base, req.Conflict, req.Choices)
		mergeMu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(200)
	})

	http.HandleFunc("POST /api/conflicts/delete", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Conflict string `json:"conflict"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		mergeMu.Lock()
		err := org.DeleteConflictFile(dir, req.Conflict)
		mergeMu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(200)
	})

	type tagEntry struct {
		*org.Entry
		DateStr string
	}

	http.HandleFunc("GET /tag/{name}", func(w http.ResponseWriter, r *http.Request) {
		tag := r.PathValue("name")
		days := 30
		if d := r.URL.Query().Get("days"); d != "" {
			if n, err := strconv.Atoi(d); err == nil && n > 0 {
				days = n
			}
		}
		entries, err := org.ParseDir(dir)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		since := time.Now().AddDate(0, 0, -days)
		var matched []tagEntry
		for _, e := range entries {
			hasTag := false
			for _, t := range e.Tags {
				if t == tag {
					hasTag = true
					break
				}
			}
			if !hasTag {
				continue
			}
			rd := e.RelevantDate()
			if rd == nil || rd.Time.Before(since) {
				continue
			}
			ds := rd.Time.Format("02 Jan 2006")
			if rd.HasTime {
				ds += " " + rd.Time.Format("15:04")
			}
			matched = append(matched, tagEntry{Entry: e, DateStr: ds})
		}
		sort.Slice(matched, func(i, j int) bool {
			return matched[j].RelevantDate().Time.Before(matched[i].RelevantDate().Time)
		})

		data := struct {
			DateStr string
			Tag     string
			Days    int
			Entries []tagEntry
		}{
			DateStr: time.Now().Format("Monday, 02 January 2006"),
			Tag:     tag,
			Days:    days,
			Entries: matched,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tagTmpl.Execute(w, data); err != nil {
			log.Printf("template: %v", err)
		}
	})

	http.HandleFunc("GET /new", func(w http.ResponseWriter, r *http.Request) {
		files, _ := org.ListFiles(dir)
		file := r.URL.Query().Get("file")
		if file == "" {
			for _, f := range files {
				if f == "inbox.org" {
					file = f
					break
				}
			}
			if file == "" && len(files) > 0 {
				file = files[0]
			}
		}
		data := editFormData{
			DateStr: time.Now().Format("Monday, 02 January 2006"),
			Mode:    "new",
			Files:   files,
			States:  []string{"TODO", "WAIT", "DONE", "CANCELED"},
			Draft:   &org.EntryDraft{File: file},
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := editTmpl.Execute(w, data); err != nil {
			log.Printf("template: %v", err)
		}
	})

	http.HandleFunc("GET /edit/{file}/{line}", func(w http.ResponseWriter, r *http.Request) {
		file := r.PathValue("file")
		line, err := strconv.Atoi(r.PathValue("line"))
		if err != nil {
			http.Error(w, "invalid line", 400)
			return
		}
		draft, err := org.LoadEntry(dir, file, line)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		files, _ := org.ListFiles(dir)
		data := editFormData{
			DateStr: time.Now().Format("Monday, 02 January 2006"),
			Mode:    "edit",
			Files:   files,
			States:  []string{"TODO", "WAIT", "DONE", "CANCELED"},
			Draft:   draft,
			TagsStr: strings.Join(draft.Tags, " "),
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := editTmpl.Execute(w, data); err != nil {
			log.Printf("template: %v", err)
		}
	})

	decodeDraft := func(r *http.Request) (*org.EntryDraft, error) {
		var req struct {
			File      string `json:"file"`
			Line      int    `json:"line"`
			State     string `json:"state"`
			Title     string `json:"title"`
			Tags      string `json:"tags"`
			Scheduled string `json:"scheduled"`
			Deadline  string `json:"deadline"`
			Body      string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, err
		}
		return &org.EntryDraft{
			File:      req.File,
			Line:      req.Line,
			State:     req.State,
			Title:     strings.TrimSpace(req.Title),
			Tags:      parseTagsInput(req.Tags),
			Scheduled: strings.TrimSpace(req.Scheduled),
			Deadline:  strings.TrimSpace(req.Deadline),
			Body:      req.Body,
		}, nil
	}

	http.HandleFunc("POST /api/new", func(w http.ResponseWriter, r *http.Request) {
		d, err := decodeDraft(r)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := org.CreateEntry(dir, d); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(200)
	})

	http.HandleFunc("POST /api/edit", func(w http.ResponseWriter, r *http.Request) {
		d, err := decodeDraft(r)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := org.UpdateEntry(dir, d); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(200)
	})

	return http.ListenAndServe(addr, nil)
}
