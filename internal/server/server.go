package server

import (
	"embed"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"orgzd/internal/agenda"
	"orgzd/internal/org"
)

//go:embed agenda.html
var tmplFS embed.FS

var tmpl = template.Must(template.ParseFS(tmplFS, "agenda.html"))

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
		if err := tmpl.Execute(w, data); err != nil {
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

	return http.ListenAndServe(addr, nil)
}
