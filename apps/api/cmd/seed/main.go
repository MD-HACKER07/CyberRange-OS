// Command seed applies migrations and loads reference data (MITRE ATT&CK,
// certification objectives, default range targets, and a Lab Admin account).
// Run: go run ./cmd/seed  [--no-embed]
package main

import (
	"context"
	"flag"

	"github.com/redis/go-redis/v9"

	"github.com/cyberrange-os/api/internal/config"
	"github.com/cyberrange-os/api/internal/db"
	"github.com/cyberrange-os/api/internal/llm"
	"github.com/cyberrange-os/api/internal/logging"
	"github.com/cyberrange-os/api/internal/seed"
)

func main() {
	noEmbed := flag.Bool("no-embed", false, "skip pgvector embedding of MITRE techniques")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logging.New(cfg.LogLevel, cfg.Env, "seed")

	ctx := context.Background()
	database, err := db.Connect(ctx, cfg.DatabaseURL, log)
	if err != nil {
		log.Fatal().Err(err).Msg("database connection failed")
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		log.Fatal().Err(err).Msg("migration failed")
	}

	// Redis is only needed by the gateway for budgets; a nil-safe client is fine.
	opt, _ := redis.ParseURL(cfg.RedisURL)
	rdb := redis.NewClient(opt)
	defer rdb.Close()

	gateway, err := llm.New(ctx, cfg, database.Pool, rdb, log)
	if err != nil {
		log.Warn().Err(err).Msg("LLM gateway unavailable; seeding without embeddings")
		gateway = nil
	}

	seeder := seed.New(database.Pool, gateway, log)
	if err := seeder.SeedAll(ctx, !*noEmbed && gateway != nil); err != nil {
		log.Fatal().Err(err).Msg("seeding failed")
	}
	log.Info().Msg("seed complete")
}
