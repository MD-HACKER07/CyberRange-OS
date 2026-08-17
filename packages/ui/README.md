# @cyberrange/ui — Vault Design System

The reusable Vault design-system components (Card, Badge, Button, StatRing,
Timer, DataTable, Terminal, SeverityBadge, AlertRow, Spinner, Input, Textarea)
are implemented in `apps/web/src/components/ui/index.tsx` and consumed across
every screen so pages never hand-roll one-off styles.

Design tokens (colors, typography, glow/scanline effects) are defined once in:

- `apps/web/tailwind.config.ts` — the `black`, `vault.*` color scales and
  animations.
- `apps/web/src/app/globals.css` — CSS variables, grid/scanline textures, and
  the live-data pulsing glow.

## Palette (spec Section 6)

| Token | Hex | Usage |
|-------|-----|-------|
| black | `#0A0A0B` | base background |
| black-soft | `#17171A` | elevated surfaces / cards |
| vault-white | `#F5F3EE` | body text (warm off-white) |
| vault-gold | `#D4AF37` | primary accent, Blue Team dominant, CTAs |
| vault-gold-bright | `#F4CD5C` | hover / glow |
| vault-red | `#C41E3A` | Red Team dominant, active/unresolved alerts |
| vault-red-deep | `#8B0000` | critical badges, Red Team tint |
| vault-slate | `#2E3440` | borders/dividers only |

Module accents: Red Team screens use red as the dominant accent; Blue Team
screens use gold, reserving red strictly for active/unresolved alerts.
