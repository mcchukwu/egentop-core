package handler

import (
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/response"
)

func MeHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		response.HandleError(w, apperrors.ErrUserNotFound)
		return
	}

	resp := map[string]any{
		"authenticated": true,
		"user_id":       userID,
	}

	response.OK(w, resp)
}
