package handler

import (
	"log/slog"
	"net/http"

	"github.com/firefly-software-mt/advanced-template/internal/view"
)

// Home handles GET / and renders the home page.
func Home() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := view.HomePage().Render(r.Context(), w); err != nil {
			slog.Error("render error", "err", err)
		}
	}
}
