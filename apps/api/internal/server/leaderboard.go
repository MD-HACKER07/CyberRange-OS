package server

import (
	"github.com/gofiber/fiber/v2"

	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/store"
)

type leaderboardHandler struct{ d *Deps }

func newLeaderboardHandler(d *Deps) *leaderboardHandler { return &leaderboardHandler{d: d} }

func (h *leaderboardHandler) register(r fiber.Router) {
	r.Get("/leaderboard/:batch_id", h.get)
}

func (h *leaderboardHandler) get(c *fiber.Ctx) error {
	bid, err := httpx.ParamUUID(c, "batch_id")
	if err != nil {
		return err
	}
	track := c.Query("track", "combined")
	rows, err := h.d.store.Leaderboard(c.Context(), bid, track)
	if err != nil {
		return httpx.Internal("failed to load leaderboard")
	}
	_ = auth.RoleStudent
	return httpx.OK(c, httpx.ListResponse[store.LeaderboardRow]{Items: rows, Total: len(rows)})
}
