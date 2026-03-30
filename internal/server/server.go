package server

import (
	"net/http"

	"foreignreader_be/internal/config"
)

func New(cfg config.Config) *http.Server {
	mux := http.NewServeMux()
	registerOperationalRoutes(mux)
	registerAPIV1Routes(mux)

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

