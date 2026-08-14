package middleware

import (
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
)

// RequirePasswordChanged gates every authenticated route except password
// change, logout, and refresh. Users provisioned with a one-time credential
// (users.must_change_password = true) must rotate it via POST /v1/me/password
// before they can use any other capability; until then every gated request
// returns 403 password_change_required.
func RequirePasswordChanged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mustChange, ok := requestctx.MustChangePassword(r.Context())
		if ok && mustChange {
			response.HandleError(w, apperrors.ErrPasswordChangeRequired)
			return
		}

		next.ServeHTTP(w, r)
	})
}
