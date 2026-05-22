package server

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"foreignreader_be/internal/analytics"
	"foreignreader_be/internal/auth"
	"foreignreader_be/internal/config"
	"foreignreader_be/internal/entitlement"
	"foreignreader_be/internal/onboardingsession"
	"foreignreader_be/internal/ratelimit"
	"foreignreader_be/internal/rateus"
	"foreignreader_be/internal/readingposition"
	"foreignreader_be/internal/translate"
)

func New(cfg config.Config, tr *translate.Client, db *sql.DB) *http.Server {
	store := auth.NewStore(db, cfg.FreeContextTranslationsPerMonth)
	issuer, err := auth.NewTokenIssuer(cfg.JWTSecret, cfg.AccessTokenTTL)
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

	rateStore := rateus.NewStore(db)
	analyticsStore := analytics.NewStore(db)
	analyticsIPWL := ratelimit.NewWindow()
	analyticsAnonWL := ratelimit.NewWindow()

	mux := http.NewServeMux()
	registerOperationalRoutes(mux)
	registerAuthRoutes(mux, cfg, store, issuer, rateStore, entStore)
	registerAdminRoutes(mux, store, issuer, entStore)
	registerEntitlementRoutes(mux, cfg, store, issuer, entStore)
	registerAPIV1Routes(mux, cfg, tr, store, issuer, entStore, obStore, sessionWL, translateIPWL, translateTokWL)
	registerUserRoutes(mux, store, issuer)
	registerReadingPositionRoutes(mux, store, issuer, entStore, rpSvc)
	registerRateUsRoutes(mux, store, issuer, rateStore)
	registerAnalyticsRoutes(mux, cfg, store, issuer, analyticsStore, analyticsIPWL, analyticsAnonWL)

	corsOrigins := newCORSOriginSet(cfg.CORSAllowedOrigins)
	handler := chain(
		mux,
		withRequestID,
		withCORS(corsOrigins),
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
