package handler

import (
	"encoding/json"
	"net/http"

	"github.com/mcchukwu/egentop/internal/middleware"
)

func Me(w http.ResponseWriter, r *http.Request) {

	userID := r.Context().Value(middleware.UserIDKey)

	resp := map[string]interface{}{
		"authenticated": true,
		"user_id":       userID,
	}

	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(resp)
}
