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
	"time"

	"orgzd/internal/agenda"
	"orgzd/internal/org"
)

//go:embed agenda.html maintenance.html tag.html
var tmplFS embed.FS

var (
	agendaTmpl = template.Must(template.ParseFS(tmplFS, "agenda.html"))
	maintTmpl  = template.Must(template.ParseFS(tmplFS, "maintenance.html"))
	tagTmpl    = template.Must(template.ParseFS(tmplFS, "tag.html"))
)

func Start(addr, dir string) error {
	http.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		entries, err := org.ParseDir(dir)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		groups := agenda.Build(entries, time.Now())
		data := struct {
			DateStr string
			Groups  []agenda.Group
		}{
			DateStr: time.Now().Format("Monday, 02 January 2006"),
			Groups:  groups,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := agendaTmpl.Execute(w, data); err != nil {
			log.Printf("template: %v", err)
		}
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

	return http.ListenAndServe(addr, nil)
}
