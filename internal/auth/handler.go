package auth

import (
	"encoding/json"
	"net/http"
)

type AuthHandler struct {
	Service *AuthService
}

func NewAuthHandler(s *AuthService) *AuthHandler {
	return &AuthHandler{Service: s}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.Service.Register(r.Context(), req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(`{"status":"registered"}`))
}
