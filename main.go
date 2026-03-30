package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"foreignreader_be/internal/config"
	"foreignreader_be/internal/server"
	"foreignreader_be/internal/translate"
)

func main() {
	cfg := config.Load()
	tr := translate.NewClient(cfg.OpenAIAPIKey, cfg.TranslateModel, cfg.TranslatePromptText, cfg.TranslateTimeout)
	srv := server.New(cfg, tr)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("env=%s port=%s", cfg.AppEnv, cfg.Port)
		log.Printf("server started")
		log.Printf("api: %s", cfg.BaseURL())
		log.Printf("health: %s/health", cfg.BaseURL())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		log.Printf("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			log.Fatalf("server error: %v", err)
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}

	log.Printf("server stopped cleanly")
}

