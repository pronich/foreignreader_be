package server

import (
	"database/sql"
	"log"
	"net/http"

	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/config"
	"foreignreader_be/internal/translate"
)

func New(cfg config.Config, tr *translate.Client, db *sql.DB) *http.Server {
	store := auth.NewStore(db)
	issuer, err := auth.NewTokenIssuer(cfg.JWTSecret)
	if err != nil {
		log.Fatalf("auth: %v", err)
	}

	mux := http.NewServeMux()
	registerOperationalRoutes(mux)
	registerAuthRoutes(mux, cfg, store, issuer)
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

