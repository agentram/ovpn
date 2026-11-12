package main

import (
	"encoding/json"
	"net/http"
	"ovpn/internal/haproxy"
)

func handleHAStatus(endpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(haproxy.Read(r.Context(), endpoint))
	}
}
