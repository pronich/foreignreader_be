package server

import (
	"database/sql"
	"net/http"

	"foreignreader_be/internal/config"
	"foreignreader_be/internal/translate"
)

func New(cfg config.Config, tr *translate.Client, db *sql.DB) *http.Server {
	_ = db // reserved for upcoming auth persistence against PostgreSQL

	mux := http.NewServeMux()
	registerOperationalRoutes(mux)
	registerAuthRoutes(mux)
	registerAPIV1Routes(mux, tr)

	handler := chain(
		mux,
		withRequestID,
		withRequestLogging,
		withRecovery,
	)

	return &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,

		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}
}

