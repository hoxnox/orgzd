package server

import (
	"embed"
	"html/template"
	"log"
	"net/http"
	"time"

	"orgzd/internal/agenda"
	"orgzd/internal/org"
)

//go:embed agenda.html
var tmplFS embed.FS

var tmpl = template.Must(template.ParseFS(tmplFS, "agenda.html"))

func Start(addr, dir string) error {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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
	return http.ListenAndServe(addr, nil)
}
