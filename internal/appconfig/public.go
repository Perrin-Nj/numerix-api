package appconfig

// The public subset of the configuration: what the player app may read without logging in.
// Nothing secret or personal lives here (no hidden item ids, no env values).

import (
	"context"
	"encoding/json"
)

type publicTrack struct {
	ID    string `json:"id"`
	Title Bi     `json:"title"`
}

type publicMusic struct {
	Enabled       bool          `json:"enabled"`
	DefaultVolume int           `json:"default_volume"`
	Tracks        []publicTrack `json:"tracks"`
}

type publicVoice struct {
	HumanEnabledDefault  bool        `json:"human_enabled_default"`
	ComputerVoiceAllowed bool        `json:"computer_voice_allowed"`
	Clips                []VoiceClip `json:"clips"`
}

type publicFeed struct {
	DiscoverEnabled    bool `json:"discover_enabled"`
	FamilyBoardVisible bool `json:"family_board_visible"`
}

// publicMaintenance mirrors Maintenance (BlockPlay lets the client show the right screen).
type publicMaintenance = Maintenance

// Public is the document served at GET /v1/config/public.
type Public struct {
	Version     int               `json:"version"` // sum of the namespace versions: changes whenever anything changes
	Music       publicMusic       `json:"music"`
	Voice       publicVoice       `json:"voice"`
	Scenes      Scenes            `json:"scenes"`
	Features    map[string]bool   `json:"features"`
	WelcomeGift bool              `json:"welcome_gift"`
	Feed        publicFeed        `json:"feed"`
	Maintenance publicMaintenance `json:"maintenance"`
	Copy        Copy              `json:"copy"`
}

type publicCache struct {
	gen  int
	at   int64 // unix seconds of the build
	body []byte
}

// PublicJSON returns the public document, built once per write generation (and rebuilt after cacheTTL).
func (s *Store) PublicJSON(ctx context.Context) ([]byte, error) {
	s.mu.RLock()
	c, gen := s.public, s.gen
	s.mu.RUnlock()
	if c != nil && c.gen == gen && s.now().Unix()-c.at < int64(cacheTTL.Seconds()) {
		return c.body, nil
	}
	p := Public{}
	ver := 0
	for _, ns := range s.order { // versions of every namespace, for the change counter
		d, err := s.Current(ctx, ns)
		if err != nil {
			return nil, err
		}
		ver += d.Version
	}
	m := Get[Music](ctx, s, NSMusic)
	p.Music = publicMusic{Enabled: m.Enabled, DefaultVolume: m.DefaultVolume, Tracks: []publicTrack{}}
	for _, t := range m.Tracks {
		if t.Enabled {
			p.Music.Tracks = append(p.Music.Tracks, publicTrack{t.ID, t.Title})
		}
	}
	v := Get[Voice](ctx, s, NSVoice)
	p.Voice = publicVoice{HumanEnabledDefault: v.HumanEnabledDefault, ComputerVoiceAllowed: v.ComputerVoiceAllowed, Clips: []VoiceClip{}}
	for _, c := range v.Clips {
		if c.Enabled {
			p.Voice.Clips = append(p.Voice.Clips, c)
		}
	}
	pl := Get[Platform](ctx, s, NSPlatform)
	f := Get[Feed](ctx, s, NSFeed)
	p.Version, p.Scenes, p.Features, p.WelcomeGift = ver, Get[Scenes](ctx, s, NSScenes), pl.Features, pl.WelcomeGift
	p.Feed = publicFeed{f.DiscoverEnabled, f.FamilyBoardVisible}
	p.Maintenance, p.Copy = Get[Maintenance](ctx, s, NSMaintenance), Get[Copy](ctx, s, NSCopy)
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.gen == gen {
		s.public = &publicCache{gen: gen, at: s.now().Unix(), body: body}
	}
	s.mu.Unlock()
	return body, nil
}
