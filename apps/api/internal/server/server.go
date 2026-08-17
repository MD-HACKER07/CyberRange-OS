// Package server wires the Fiber app: middleware, health, metrics, and every
// module's routes. Handlers are grouped per module but share one Deps struct.
package server

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/cyberrange-os/api/internal/audit"
	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/config"
	"github.com/cyberrange-os/api/internal/db"
	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/ingest"
	"github.com/cyberrange-os/api/internal/llm"
	"github.com/cyberrange-os/api/internal/metrics"
	"github.com/cyberrange-os/api/internal/mitre"
	"github.com/cyberrange-os/api/internal/orchestrator"
	"github.com/cyberrange-os/api/internal/realtime"
	"github.com/cyberrange-os/api/internal/store"
)

type Deps struct {
	Cfg     *config.Config
	Log     zerolog.Logger
	DB      *db.DB
	Redis   *redis.Client
	Audit   *audit.Logger
	Issuer  *auth.TokenIssuer
	Hub     *realtime.Hub
	Gateway *llm.Gateway

	// Constructed inside New.
	authStore    *auth.Store
	store        *store.Store
	provisioner  orchestrator.RangeProvisioner
	mitreEngine  *mitre.Engine
	closers      []func()
}

func (d *Deps) Shutdown() {
	for _, c := range d.closers {
		c()
	}
}

func New(d *Deps) *fiber.App {
	d.authStore = auth.NewStore(d.DB.Pool)
	d.store = store.New(d.DB.Pool)
	d.mitreEngine = mitre.NewEngine(d.DB.Pool, d.Gateway, d.Log)

	// Blue Team telemetry: poll Wazuh + tail Suricata into the alert stream.
	ingestCtx, ingestCancel := context.WithCancel(context.Background())
	d.closers = append(d.closers, ingestCancel)
	ingestSvc := ingest.New(d.Cfg, d.store, d.Hub, d.mitreEngine, d.Log)
	ingestSvc.Start(ingestCtx)

	// Range provisioner driver selection.
	switch d.Cfg.RangeDriver {
	case "proxmox":
		d.provisioner = orchestrator.NewProxmoxDriver(d.Cfg, d.Log)
	default:
		drv, err := orchestrator.NewDockerDriver(d.Cfg.DockerHost, d.Cfg.DockerAPIVersion, d.Cfg.RangeSubnetPrefix, d.Log)
		if err != nil {
			d.Log.Error().Err(err).Msg("docker driver init failed; range provisioning will be unavailable")
		} else {
			d.provisioner = drv
		}
	}

	app := fiber.New(fiber.Config{
		AppName:               "CyberRange OS API",
		ErrorHandler:          httpx.ErrorHandler,
		DisableStartupMessage: true,
		ReadTimeout:           30 * time.Second,
		WriteTimeout:          0, // streaming endpoints
		BodyLimit:             25 * 1024 * 1024,
	})

	app.Use(recover.New())
	app.Use(requestLogger(d.Log))
	app.Use(cors.New(cors.Config{
		AllowOrigins:     d.Cfg.PublicURL,
		AllowCredentials: true,
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS",
	}))
	if d.Cfg.MetricsEnabled {
		app.Use(metrics.Middleware())
		app.Get("/metrics", metrics.Handler())
	}

	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "time": time.Now().UTC()})
	})
	app.Get("/readyz", d.readiness)

	api := app.Group("/api")
	registerRoutes(d, api)
	return app
}

func (d *Deps) readiness(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
	defer cancel()
	out := fiber.Map{}
	dbOK := d.DB.Ping(ctx) == nil
	redisOK := d.Redis.Ping(ctx).Err() == nil
	out["database"] = dbOK
	out["redis"] = redisOK
	out["llm_egress"] = d.Gateway.Assertion()
	status := fiber.StatusOK
	if !dbOK {
		status = fiber.StatusServiceUnavailable
	}
	return c.Status(status).JSON(out)
}

func requestLogger(log zerolog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		log.Debug().
			Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", c.Response().StatusCode()).
			Dur("dur", time.Since(start)).
			Msg("request")
		return err
	}
}
