// Package feedmedia holds the curated Discover items: their shape, their validation, the database sync used by
// cmd/feedseed, the per-child random order and the file server for the media directory.
//
// Go for Spring people: this package imports nothing from the rest of the repo except store, so it is easy to test.
// A struct with `json:"..."` tags is like a Jackson DTO: the tag is the JSON property name.
package feedmedia

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Text is what a child reads, in one language.
type Text struct {
	Title    string `json:"title"`
	Caption  string `json:"caption"` // the fun fact
	Alt      string `json:"alt"`     // what the media shows, for screen readers
	Question string `json:"question,omitempty"`
	Answer   string `json:"answer,omitempty"`
}

// Caption is one WebVTT track of a video.
type Caption struct {
	Lang  string `json:"lang"`
	Label string `json:"label"`
	Src   string `json:"src"` // file name inside MEDIA_DIR
}

// Item is one curated Discover item (a row of feed_media, body column plus the columns beside it).
type Item struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"` // photo | animation | video
	World       string    `json:"world"`
	Topic       string    `json:"topic"`
	GradeMin    int       `json:"grade_min"`
	Tags        []string  `json:"tags"`
	FR          Text      `json:"fr"`
	EN          Text      `json:"en"`
	DurationMS  int       `json:"duration_ms,omitempty"`
	Src         string    `json:"src"`
	SrcSmall    string    `json:"src_small,omitempty"`
	Poster      string    `json:"poster,omitempty"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	LQIP        string    `json:"lqip"`
	HasAudio    bool      `json:"has_audio,omitempty"`
	Captions    []Caption `json:"captions,omitempty"`
	MotionNote  string    `json:"motion_note,omitempty"`
	SourceURL   string    `json:"source_url"`
	Creator     string    `json:"creator"`
	Licence     string    `json:"licence"`
	LicenceURL  string    `json:"licence_url,omitempty"`
	Attribution string    `json:"attribution"`
}

// Kinds of media item.
const (
	KindPhoto     = "photo"
	KindAnimation = "animation"
	KindVideo     = "video"
)

// worlds are the twenty world ids of the contract (WorldId).
var worlds = map[string]bool{
	"nature": true, "espace": true, "cuisine": true, "sport": true, "construction": true, "medecine": true, "science": true,
	"echecs": true, "ocean": true, "dinosaures": true, "animaux": true, "musique": true, "transport": true, "pirates": true,
	"marche": true, "meteo": true, "robots": true, "arts": true, "hiver": true, "jeux": true,
}

var (
	idRe      = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	fileRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,100}\.(webp|jpg|mp4|webm|vtt)$`)
	licenceRe = regexp.MustCompile(`^(CC0 1\.0|Public Domain Mark 1\.0|Public domain|CC BY (2\.0|2\.5|3\.0|4\.0))$`)
)

// FileRe is the file name pattern shared with the contract (path parameter of getFeedMedia).
func FileRe() *regexp.Regexp { return fileRe }

// Validate returns every problem of the item (empty means valid). dir is MEDIA_DIR; when not empty each file must exist.
func (it Item) Validate(dir string) []string {
	var p []string
	bad := func(format string, a ...any) { p = append(p, it.ID+": "+fmt.Sprintf(format, a...)) }
	if !idRe.MatchString(it.ID) || len(it.ID) > 48 {
		bad("id must be a lowercase slug")
	}
	switch it.Kind {
	case KindPhoto, KindAnimation, KindVideo:
	default:
		bad("kind %q unknown", it.Kind)
	}
	if !worlds[it.World] {
		bad("world %q unknown", it.World)
	}
	if it.Topic == "" || len(it.Tags) == 0 {
		bad("topic and tags are required")
	}
	if it.GradeMin < 1 || it.GradeMin > 6 {
		bad("grade_min must be 1..6")
	}
	for lang, t := range map[string]Text{"fr": it.FR, "en": it.EN} {
		if t.Title == "" || t.Caption == "" || t.Alt == "" {
			bad("%s title, caption and alt are required", lang)
		}
		if (t.Question == "") != (t.Answer == "") {
			bad("%s question and answer go together", lang)
		}
		for _, s := range []string{t.Title, t.Caption, t.Alt, t.Question, t.Answer} {
			if strings.ContainsAny(s, "—–") { // kid-facing copy never uses em or en dashes
				bad("%s text contains a dash character", lang)
			}
		}
	}
	if (it.FR.Question == "") != (it.EN.Question == "") {
		bad("the question must exist in both languages or neither")
	}
	if it.Width <= 0 || it.Height <= 0 || !strings.HasPrefix(it.LQIP, "data:image/") {
		bad("width, height and lqip are required")
	}
	if !strings.HasPrefix(it.SourceURL, "https://") || it.Creator == "" || it.Attribution == "" {
		bad("source_url (https), creator and attribution are required")
	}
	if !licenceRe.MatchString(it.Licence) {
		bad("licence %q is not CC0, public domain or CC BY", it.Licence)
	}
	files := []string{it.Src, it.SrcSmall, it.Poster}
	switch it.Kind {
	case KindPhoto:
		if it.SrcSmall == "" || !strings.HasSuffix(it.Src, ".webp") {
			bad("a photo needs src (webp) and src_small")
		}
	case KindAnimation, KindVideo:
		if !strings.HasSuffix(it.Src, ".mp4") || it.Poster == "" || it.DurationMS < 2000 || it.DurationMS > 30000 {
			bad("an animation or video needs an mp4 src, a poster and a duration of 2 to 30 s")
		}
	}
	if it.Kind == KindVideo {
		langs := map[string]bool{}
		for _, c := range it.Captions {
			langs[c.Lang] = true
			files = append(files, c.Src)
		}
		if !langs["fr-CA"] || !langs["en-CA"] {
			bad("a video needs captions in fr-CA and en-CA")
		}
	}
	for _, f := range files {
		if f == "" {
			continue
		}
		if !fileRe.MatchString(f) {
			bad("file name %q is not allowed", f)
			continue
		}
		if dir != "" {
			if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
				bad("file %q is missing in the media directory", f)
			}
		}
	}
	return p
}
