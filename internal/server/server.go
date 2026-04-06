package server

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/config"
	"foreignreader_be/internal/entitlement"
	"foreignreader_be/internal/onboardingsession"
	"foreignreader_be/internal/ratelimit"
	"foreignreader_be/internal/readingposition"
	"foreignreader_be/internal/translate"
)

func New(cfg config.Config, tr *translate.Client, db *sql.DB) *http.Server {
	store := auth.NewStore(db, cfg.FreeContextTranslationsPerMonth)
	issuer, err := auth.NewTokenIssuer(cfg.JWTSecret)
	if err != nil {
		log.Fatalf("auth: %v", err)
	}
	entStore := entitlement.NewStore(db)
	obStore := onboardingsession.NewStore(db)
	sessionWL := ratelimit.NewWindow()
	translateIPWL := ratelimit.NewWindow()
	translateTokWL := ratelimit.NewWindow()
	obStore.RunPeriodicCleanup(time.Hour, cfg.OnboardingTokenCleanupRetention)

	rpRepo := readingposition.NewRepository(db)
	rpSvc := readingposition.NewService(rpRepo)

	mux := http.NewServeMux()
	registerOperationalRoutes(mux)
	registerAuthRoutes(mux, cfg, store, issuer)
	registerEntitlementRoutes(mux, cfg, store, issuer, entStore)
	registerAPIV1Routes(mux, cfg, tr, store, issuer, entStore, obStore, sessionWL, translateIPWL, translateTokWL)
	registerReadingPositionRoutes(mux, store, issuer, entStore, rpSvc)

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
