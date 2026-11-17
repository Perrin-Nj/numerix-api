package adminapi

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/Perrin-Nj/numerix-api/internal/httpapi"
	"github.com/Perrin-Nj/numerix-api/internal/pwreset"
)

// POST /admin/v1/parents/{id}/password-reset: one-time 8 digit code valid 1 hour, earlier codes burned.
// The admin reads the code to the parent; it is returned once and never logged or audited.
func (a *API) passwordReset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var email, code string
	err := a.db.WithWriteTx(r.Context(), func(tx *sql.Tx) (err error) {
		if err = tx.QueryRow(`SELECT email FROM parents WHERE id = ? AND deleted_at IS NULL`, id).Scan(&email); errors.Is(err, sql.ErrNoRows) {
			return httpapi.ErrNotFound
		} else if err != nil {
			return err
		}
		if code, err = pwreset.Issue(tx, id, a.now()); err != nil {
			return err
		}
		return a.writeAudit(tx, actor(r), "parent.password_reset", "parent", id, "reset code created, valid 1 hour")
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{
		"code":               code,
		"expires_in_seconds": int(pwreset.TTL.Seconds()),
		"parent":             map[string]string{"id": id, "email": email},
	})
}
