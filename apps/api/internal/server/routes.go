package server

import (
	"github.com/gofiber/fiber/v2"

	"github.com/cyberrange-os/api/internal/auth"
)

func registerRoutes(d *Deps, api fiber.Router) {
	authHandler := auth.NewHandler(d.Cfg, d.authStore, d.Issuer, d.Audit, d.Log)
	authHandler.Register(api)

	// Everything below requires a valid access token.
	protected := api.Group("", d.Issuer.Middleware(), auth.DenyAuditorWrites())
	protected.Get("/me", authHandler.Me)

	newCourseHandler(d).register(protected)
	newBatchHandler(d).register(protected)
	newExerciseHandler(d).register(protected)
	newRangeHandler(d).register(protected)
	newSIEMHandler(d).register(protected)
	newReportHandler(d).register(protected)
	newAnalyticsHandler(d).register(protected)
	newLeaderboardHandler(d).register(protected)
	newCertHandler(d).register(protected)
	newMitreHandler(d).register(protected)
	newAISecHandler(d).register(protected)
	newAdminHandler(d).register(protected)
}
