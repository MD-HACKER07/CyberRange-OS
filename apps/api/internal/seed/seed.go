// Package seed bootstraps reference data: the MITRE ATT&CK dataset (with
// pgvector embeddings), certification objective lists, the default set of
// intentionally-vulnerable range targets, and an initial Lab Admin account.
package seed

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/cyberrange-os/api/internal/auth"
	"github.com/cyberrange-os/api/internal/llm"
)

//go:embed data/*.json
var dataFS embed.FS

type Seeder struct {
	pool    *pgxpool.Pool
	gateway *llm.Gateway
	log     zerolog.Logger
}

func New(pool *pgxpool.Pool, gateway *llm.Gateway, log zerolog.Logger) *Seeder {
	return &Seeder{pool: pool, gateway: gateway, log: log}
}

type techniqueRecord struct {
	TechniqueID    string   `json:"technique_id"`
	Name           string   `json:"name"`
	Tactic         string   `json:"tactic"`
	Description    string   `json:"description"`
	IsSubtechnique bool     `json:"is_subtechnique"`
	ParentID       string   `json:"parent_id"`
	Platforms      []string `json:"platforms"`
}

// SeedMITRE loads techniques from the embedded curated set, or from a full
// STIX bundle path when MITRE_STIX_PATH is set, then computes embeddings.
func (s *Seeder) SeedMITRE(ctx context.Context, embed bool) error {
	var records []techniqueRecord
	if path := strings.TrimSpace(os.Getenv("MITRE_STIX_PATH")); path != "" {
		parsed, err := parseSTIX(path)
		if err != nil {
			return fmt.Errorf("parse STIX bundle: %w", err)
		}
		records = parsed
		s.log.Info().Int("count", len(records)).Str("path", path).Msg("loaded MITRE from STIX bundle")
	} else {
		raw, err := dataFS.ReadFile("data/mitre_core.json")
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &records); err != nil {
			return err
		}
		s.log.Info().Int("count", len(records)).Msg("loaded curated MITRE technique set")
	}

	for _, r := range records {
		if r.Platforms == nil {
			r.Platforms = []string{}
		}
		url := "https://attack.mitre.org/techniques/" + strings.ReplaceAll(r.TechniqueID, ".", "/")
		var parent *string
		if r.ParentID != "" {
			parent = &r.ParentID
		}
		_, err := s.pool.Exec(ctx, `
			INSERT INTO mitre_techniques (technique_id, name, tactic, tactics, description, is_subtechnique, parent_id, url, platforms)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (technique_id) DO UPDATE SET
				name=EXCLUDED.name, tactic=EXCLUDED.tactic, description=EXCLUDED.description,
				platforms=EXCLUDED.platforms, updated_at=now()`,
			r.TechniqueID, r.Name, r.Tactic, []string{r.Tactic}, r.Description, r.IsSubtechnique, parent, url, r.Platforms)
		if err != nil {
			return fmt.Errorf("insert technique %s: %w", r.TechniqueID, err)
		}
	}

	if embed && s.gateway != nil {
		s.embedTechniques(ctx)
	}
	return nil
}

func (s *Seeder) embedTechniques(ctx context.Context) {
	rows, err := s.pool.Query(ctx, `SELECT technique_id, name, description FROM mitre_techniques WHERE embedding IS NULL`)
	if err != nil {
		s.log.Warn().Err(err).Msg("could not query techniques for embedding")
		return
	}
	type item struct{ id, text string }
	items := []item{}
	for rows.Next() {
		var id, name, desc string
		if err := rows.Scan(&id, &name, &desc); err == nil {
			items = append(items, item{id, name + ": " + desc})
		}
	}
	rows.Close()

	embedded := 0
	for _, it := range items {
		vec, err := s.gateway.Embed(ctx, it.text)
		if err != nil {
			s.log.Warn().Err(err).Msg("embedding model unavailable; skipping vector seed (lexical fallback stays active)")
			return
		}
		lit := vectorLiteral(vec)
		if _, err := s.pool.Exec(ctx, `UPDATE mitre_techniques SET embedding=$2::vector WHERE technique_id=$1`, it.id, lit); err != nil {
			s.log.Warn().Err(err).Str("id", it.id).Msg("failed to store embedding")
			continue
		}
		embedded++
	}
	s.log.Info().Int("embedded", embedded).Msg("technique embeddings computed")
}

func (s *Seeder) SeedCertObjectives(ctx context.Context) error {
	raw, err := dataFS.ReadFile("data/cert_objectives.json")
	if err != nil {
		return err
	}
	var objs []struct {
		Cert        string `json:"cert"`
		Code        string `json:"code"`
		Domain      string `json:"domain"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &objs); err != nil {
		return err
	}
	for _, o := range objs {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO cert_objectives (cert, code, domain, description)
			VALUES ($1,$2,$3,$4) ON CONFLICT (cert, code) DO UPDATE SET description=EXCLUDED.description`,
			o.Cert, o.Code, o.Domain, o.Description); err != nil {
			return err
		}
	}
	s.log.Info().Int("count", len(objs)).Msg("certification objectives seeded")
	return nil
}

// DefaultTarget is one of the standard intentionally-vulnerable images.
type DefaultTarget struct {
	Slug         string
	Name         string
	Description  string
	Image        string
	ExposedPorts []int32
	MemoryMB     int
}

var DefaultTargets = []DefaultTarget{
	{"metasploitable2", "Metasploitable 2", "Classic intentionally-vulnerable Linux VM with many exploitable services.", "tleemcjr/metasploitable2:latest", []int32{21, 22, 23, 80, 139, 445, 3306, 5432}, 1024},
	{"dvwa", "DVWA", "Damn Vulnerable Web Application — configurable web vulnerabilities.", "vulnerables/web-dvwa:latest", []int32{80}, 512},
	{"juice-shop", "OWASP Juice Shop", "Modern vulnerable web app covering the OWASP Top 10.", "bkimminich/juice-shop:latest", []int32{3000}, 1024},
	{"webgoat", "WebGoat", "OWASP WebGoat deliberately insecure web application for lessons.", "webgoat/webgoat:latest", []int32{8080}, 1024},
}

func (s *Seeder) SeedRangeTargets(ctx context.Context) error {
	for _, t := range DefaultTargets {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO range_targets (slug, name, description, image, exposed_ports, cpu_quota, memory_mb)
			VALUES ($1,$2,$3,$4,$5,1.0,$6)
			ON CONFLICT (slug) DO NOTHING`,
			t.Slug, t.Name, t.Description, t.Image, t.ExposedPorts, t.MemoryMB); err != nil {
			return err
		}
	}
	s.log.Info().Int("count", len(DefaultTargets)).Msg("default range targets registered")
	return nil
}

// SeedAdmin bootstraps a Lab Admin from SEED_ADMIN_EMAIL / SEED_ADMIN_PASSWORD
// if no admin exists yet.
func (s *Seeder) SeedAdmin(ctx context.Context) error {
	var admins int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin'`).Scan(&admins); err != nil {
		return err
	}
	if admins > 0 {
		return nil
	}
	email := strings.TrimSpace(os.Getenv("SEED_ADMIN_EMAIL"))
	password := os.Getenv("SEED_ADMIN_PASSWORD")
	name := strings.TrimSpace(os.Getenv("SEED_ADMIN_NAME"))
	if email == "" {
		email = "admin@cyberrange.local"
	}
	if name == "" {
		name = "Lab Admin"
	}
	if password == "" {
		password = auth.RandomSecret(9)
		s.log.Warn().Str("email", email).Str("generated_password", password).
			Msg("no SEED_ADMIN_PASSWORD set; generated a temporary admin password (change it immediately)")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO users (role, name, email, password_hash, auth_provider)
		VALUES ('admin',$1,lower($2),$3,'local')`, name, email, hash); err != nil {
		return err
	}
	s.log.Info().Str("email", email).Msg("bootstrapped Lab Admin account")
	return nil
}

func (s *Seeder) SeedAll(ctx context.Context, embed bool) error {
	if err := s.SeedAdmin(ctx); err != nil {
		return fmt.Errorf("admin: %w", err)
	}
	if err := s.SeedRangeTargets(ctx); err != nil {
		return fmt.Errorf("targets: %w", err)
	}
	if err := s.SeedCertObjectives(ctx); err != nil {
		return fmt.Errorf("certs: %w", err)
	}
	if err := s.SeedMITRE(ctx, embed); err != nil {
		return fmt.Errorf("mitre: %w", err)
	}
	return nil
}

func vectorLiteral(vec []float32) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = fmt.Sprintf("%g", v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
