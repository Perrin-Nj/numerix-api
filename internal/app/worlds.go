package app

// The world registry. A world is whatever table `worlds` says: `published` worlds are open, `draft` worlds exist
// but no child or parent can reach them yet, `retired` ones are gone. Which published worlds a new child starts
// with, and which ones a parent may switch on, are platform settings (default_worlds, ready_worlds).

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
)

// worldStatus returns the status of a world ("" when it does not exist).
func (s *Server) worldStatus(ctx context.Context, id string) (string, error) {
	var st string
	err := s.Store.R.QueryRowContext(ctx, `SELECT status FROM worlds WHERE id = ? AND status <> 'retired'`, id).Scan(&st)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", s.plInternalErr(ctx, "check world", err)
	}
	return st, nil
}

// worldInactive is the 422 for a world that exists but is not open yet.
func worldInactive(id string) *httpapi.APIError {
	return httpapi.NewError(http.StatusUnprocessableEntity, "world_inactive", "The world "+id+" is not open yet.")
}

// worldOpen is nil for a published world, a validation error for an unknown one and world_inactive for a draft.
func (s *Server) worldOpen(ctx context.Context, id string) error {
	st, err := s.worldStatus(ctx, id)
	switch {
	case err != nil:
		return err
	case st == "":
		return httpapi.Validation(plField("world", "unknown world"))
	case st != "published":
		return worldInactive(id)
	}
	return nil
}

// defaultWorlds are the worlds a new child starts with (platform.default_worlds).
func (s *Server) defaultWorlds(ctx context.Context) []string {
	return slices.Clone(s.plConfig(ctx).DefaultWorlds)
}

// checkSettingsWorlds validates the worlds a parent wants enabled: each one must exist, be published, and be ready
// (platform.ready_worlds) unless the child already had it switched on, because stored settings are never rewritten.
func (s *Server) checkSettingsWorlds(ctx context.Context, childID string, want []string) error {
	ready := s.plConfig(ctx).ReadyWorlds
	var current []string
	if st, err := s.storedWorlds(ctx, childID); err == nil {
		current = st
	}
	for _, w := range want {
		st, err := s.worldStatus(ctx, w)
		if err != nil {
			return err
		}
		switch {
		case st == "":
			return httpapi.Validation(plField("enabled_worlds", "unknown or repeated world"))
		case st != "published":
			return worldInactive(w)
		case !slices.Contains(ready, w) && !slices.Contains(current, w):
			return httpapi.NewError(http.StatusUnprocessableEntity, "world_not_ready", "The world "+w+" is not available for your family yet.")
		}
	}
	return nil
}

// openWorlds is the set of published world ids, cached for 30 seconds and until content changes (an admin status
// switch bumps the content generation, so the change shows at once).
func (s *Server) openWorlds(ctx context.Context) map[string]bool {
	st := s.cs()
	gen := s.Content.Cache.Generation()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.open != nil && st.openGen == gen && time.Since(st.openAt) < 30*time.Second {
		return st.open
	}
	rows, err := s.Store.R.QueryContext(ctx, `SELECT id FROM worlds WHERE status = 'published'`)
	if err != nil {
		s.Log.Warn("list published worlds failed", "err", err)
		return st.open // keep the old set (nil = no filtering)
	}
	defer rows.Close()
	open := map[string]bool{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			open[id] = true
		}
	}
	st.open, st.openAt, st.openGen = open, time.Now(), gen
	return open
}
