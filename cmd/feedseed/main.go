// Command feedseed loads the curated Discover items (seed/feed/items.json) into the database.
// Idempotent: unchanged items are skipped, removed items are deleted. The media files stay on disk (MEDIA_DIR).
//
//	feedseed --db data/numerix.db --items seed/feed/items.json --media seed/feed-media
//	feedseed --validate-only      check the file and the media directory, never open the database
//
// Go for Spring people: a second main package, like a CommandLineRunner packaged on its own. run() returns the exit
// code so tests can call it.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/feedmedia"
	"github.com/Perrin-Nj/numerix-api/internal/store"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv)) }

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("feedseed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	def := func(k, v string) string {
		if e := getenv(k); e != "" {
			return e
		}
		return v
	}
	db := fs.String("db", def("DB_PATH", "./data/numerix.db"), "SQLite database file")
	items := fs.String("items", "seed/feed/items.json", "the items file")
	media := fs.String("media", def("MEDIA_DIR", "seed/feed-media"), "media directory the files must exist in")
	validate := fs.Bool("validate-only", false, "validate only")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	raw, err := os.ReadFile(*items)
	if err != nil {
		fmt.Fprintln(stderr, "read items:", err)
		return 1
	}
	list, problems := feedmedia.ParseAll(raw, *media)
	for _, p := range problems {
		fmt.Fprintln(stderr, "invalid:", p)
	}
	if len(problems) > 0 {
		return 1
	}
	if *validate {
		fmt.Fprintf(stdout, "%d items valid\n", len(list))
		return 0
	}
	ctx := context.Background()
	s, err := store.Open(ctx, *db) // Open also runs the migrations
	if err != nil {
		fmt.Fprintln(stderr, "open db:", err)
		return 1
	}
	defer s.Close()
	res, err := feedmedia.Apply(ctx, s, list, time.Now())
	if err != nil {
		fmt.Fprintln(stderr, "apply:", err)
		return 1
	}
	fmt.Fprintf(stdout, "feed media: %d inserted, %d updated, %d unchanged, %d deleted\n", res.Inserted, res.Updated, res.Unchanged, res.Deleted)
	return 0
}
