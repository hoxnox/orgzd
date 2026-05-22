package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"orgzd/internal/agenda"
	"orgzd/internal/org"
	"orgzd/internal/server"
)

func main() {
	home, _ := os.UserHomeDir()
	dir := flag.String("dir", filepath.Join(home, "todo"), "directory with org files")
	listen := flag.String("listen", ":8042", "HTTP listen address (host:port; :PORT for all interfaces)")
	cli := flag.Bool("cli", false, "print agenda to stdout and exit")
	flag.Parse()

	if *cli {
		printAgenda(*dir)
		return
	}

	log.Printf("orgzd listen=%s dir=%s", *listen, *dir)
	if err := server.Start(*listen, *dir); err != nil {
		log.Fatal(err)
	}
}

func printAgenda(dir string) {
	entries, err := org.ParseDir(dir)
	if err != nil {
		log.Fatal(err)
	}
	groups := agenda.Build(entries, time.Now())
	for _, g := range groups {
		color := "34"
		if g.Type == "overdue" {
			color = "31"
		}
		fmt.Printf("\n\033[1;%sm%s\033[0m (%d)\n", color, g.Label, len(g.Entries))
		if len(g.Entries) == 0 {
			fmt.Println("  \033[90m—\033[0m")
			continue
		}
		for _, e := range g.Entries {
			var parts []string
			if e.State != "" {
				parts = append(parts, fmt.Sprintf("\033[33m%-4s\033[0m", e.State))
			} else {
				parts = append(parts, "     ")
			}
			parts = append(parts, fmt.Sprintf("%-40s", e.Title))
			if e.HasTime {
				parts = append(parts, fmt.Sprintf("\033[36m%s\033[0m", e.Date.Format("15:04")))
			} else {
				parts = append(parts, fmt.Sprintf("\033[36m%s\033[0m", e.Date.Format("02 Jan")))
			}
			if len(e.Tags) > 0 {
				parts = append(parts, fmt.Sprintf("\033[90m:%s:\033[0m", strings.Join(e.Tags, ":")))
			}
			parts = append(parts, fmt.Sprintf("\033[90m%s\033[0m", e.File))
			fmt.Printf("  %s\n", strings.Join(parts, " "))
		}
	}
	fmt.Println()
}
