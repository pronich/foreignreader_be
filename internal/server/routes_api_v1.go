package server

import (
	"encoding/json"
	"net/http"
)

func registerAPIV1Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/translate/context", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "not_implemented",
			"message": "endpoint not implemented yet",
		})
	})
}

