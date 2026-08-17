package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	"github.com/cyberrange-os/api/internal/audit"
	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/config"
	"github.com/cyberrange-os/api/internal/db"
	"github.com/cyberrange-os/api/internal/llm"
	"github.com/cyberrange-os/api/internal/logging"
	"github.com/cyberrange-os/api/internal/realtime"
	"github.com/cyberrange-os/api/internal/seed"
	"github.com/cyberrange-os/api/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logging.New(cfg.LogLevel, cfg.Env, "api")
	log.Info().Str("env", cfg.Env).Msg("CyberRange OS API starting")

	ctx := context.Background()

	database, err := db.Connect(ctx, cfg.DatabaseURL, log)
	if err != nil {
		log.Fatal().Err(err).Msg("database connection failed")
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		log.Fatal().Err(err).Msg("migration failed")
	}

	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatal().Err(err).Msg("invalid REDIS_URL")
	}
	if cfg.RedisPassword != "" {
		opt.Password = cfg.RedisPassword
	}
	rdb := redis.NewClient(opt)
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Warn().Err(err).Msg("redis unreachable at startup; realtime falls back to local delivery")
	}
	defer rdb.Close()

	// LLM Gateway runs the local-inference egress guard before anything else.
	gateway, err := llm.New(ctx, cfg, database.Pool, rdb, log)
	if err != nil {
		log.Fatal().Err(err).Msg("LLM gateway initialization failed (local-inference guard)")
	}

	// Idempotently seed reference data so a fresh deployment is usable end to
	// end. Embeddings are attempted only if the local model answers.
	seeder := seed.New(database.Pool, gateway, log)
	if err := seeder.SeedAll(ctx, true); err != nil {
		log.Warn().Err(err).Msg("reference data seeding incomplete (continuing)")
	}

	auditor := audit.New(database.Pool, log)
	issuer := auth.NewTokenIssuer(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	hub := realtime.NewHub(rdb, log)

	deps := &server.Deps{
		Cfg:     cfg,
		Log:     log,
		DB:      database,
		Redis:   rdb,
		Audit:   auditor,
		Issuer:  issuer,
		Hub:     hub,
		Gateway: gateway,
	}

	app := server.New(deps)

	go func() {
		if err := app.Listen(cfg.HTTPAddr); err != nil && !errors.Is(err, fiber.ErrServiceUnavailable) {
			log.Error().Err(err).Msg("http server stopped")
		}
	}()
	log.Info().Str("addr", cfg.HTTPAddr).Msg("listening")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Info().Msg("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_ = app.ShutdownWithContext(shutdownCtx)
	deps.Shutdown()
}
