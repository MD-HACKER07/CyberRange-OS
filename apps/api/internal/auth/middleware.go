package auth

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/cyberrange-os/api/internal/httpx"
)

type Identity struct {
	UserID uuid.UUID `json:"id"`
	Role   Role      `json:"role"`
	Name   string    `json:"name"`
	RollNo string    `json:"roll_no,omitempty"`
}

const (
	localsIdentity = "identity"
	localsUserID   = "user_id"
	localsRole     = "user_role"
)

// Middleware validates the bearer access token (or the ws access_token query
// param used by browser WebSocket clients, which cannot set headers).
func (t *TokenIssuer) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		raw := extractToken(c)
		if raw == "" {
			return httpx.Unauthorized("missing access token")
		}
		claims, err := t.Parse(raw)
		if err != nil {
			return httpx.Unauthorized("invalid or expired access token")
		}
		id := Identity{UserID: claims.UserID, Role: claims.Role, Name: claims.Name, RollNo: claims.RollNo}
		c.Locals(localsIdentity, id)
		c.Locals(localsUserID, claims.UserID)
		c.Locals(localsRole, string(claims.Role))
		return c.Next()
	}
}

// Optional attaches identity when a token is present but never rejects.
func (t *TokenIssuer) Optional() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if raw := extractToken(c); raw != "" {
			if claims, err := t.Parse(raw); err == nil {
				id := Identity{UserID: claims.UserID, Role: claims.Role, Name: claims.Name, RollNo: claims.RollNo}
				c.Locals(localsIdentity, id)
				c.Locals(localsUserID, claims.UserID)
				c.Locals(localsRole, string(claims.Role))
			}
		}
		return c.Next()
	}
}

func extractToken(c *fiber.Ctx) string {
	h := c.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	if q := c.Query("access_token"); q != "" {
		return strings.TrimSpace(q)
	}
	if ck := c.Cookies("access_token"); ck != "" {
		return ck
	}
	return ""
}

// RequireRole gates a route to a set of roles.
func RequireRole(roles ...Role) fiber.Handler {
	allowed := make(map[Role]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *fiber.Ctx) error {
		id, ok := Current(c)
		if !ok {
			return httpx.Unauthorized("authentication required")
		}
		if !allowed[id.Role] {
			return httpx.Forbidden("role " + string(id.Role) + " is not permitted to perform this action")
		}
		return c.Next()
	}
}

// DenyAuditorWrites blocks every mutating verb for the read-only auditor role.
func DenyAuditorWrites() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, ok := Current(c)
		if ok && id.Role == RoleAuditor {
			switch c.Method() {
			case fiber.MethodPost, fiber.MethodPut, fiber.MethodPatch, fiber.MethodDelete:
				return httpx.Forbidden("auditor role is read-only")
			}
		}
		return c.Next()
	}
}

func Current(c *fiber.Ctx) (Identity, bool) {
	v := c.Locals(localsIdentity)
	if v == nil {
		return Identity{}, false
	}
	id, ok := v.(Identity)
	return id, ok
}

func MustCurrent(c *fiber.Ctx) (Identity, error) {
	id, ok := Current(c)
	if !ok {
		return Identity{}, httpx.Unauthorized("authentication required")
	}
	return id, nil
}

func IsStaff(r Role) bool { return r == RoleFaculty || r == RoleAdmin }
