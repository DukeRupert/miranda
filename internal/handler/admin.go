package handler

import (
	"log/slog"
	"net/http"

	"github.com/dukerupert/miranda/internal/session"
	"github.com/dukerupert/miranda/internal/view"
)

// AdminDashboard handles GET /admin. It assumes session.RequireAuth has
// already run, so the user is always present in the context.
func AdminDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := session.FromContext(r.Context())
		if err := view.AdminPage(user).Render(r.Context(), w); err != nil {
			slog.Error("render error", "err", err)
		}
	}
}
