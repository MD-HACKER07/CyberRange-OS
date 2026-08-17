// Package config loads all runtime configuration from the environment.
// Nothing in the platform is hardcoded: every external dependency
// (Postgres, Redis, the local LLM endpoint, Wazuh, Docker/Proxmox) is
// addressed through a configurable URL so the same build runs in the dev
// lab and on the department's production hardware.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Env      string
	HTTPAddr string
	PublicURL string

	// Postgres
	DatabaseURL string

	// Redis
	RedisURL      string
	RedisPassword string

	// Auth
	JWTSecret          string
	AccessTokenTTL     time.Duration
	RefreshTokenTTL    time.Duration
	CookieDomain       string
	CookieSecure       bool
	OIDCIssuer         string
	OIDCClientID       string
	OIDCClientSecret   string
	OIDCRedirectURL    string
	SAMLMetadataURL    string
	AllowLocalPassword bool

	// Local LLM runtime (Ollama / vLLM / TGI) — never a cloud API.
	LLMBaseURL          string
	LLMDefaultModel     string
	LLMEmbedModel       string
	LLMRequestTimeout   time.Duration
	LLMAllowPublicIP    bool // must stay false outside of explicitly-approved lab setups
	LLMSessionTokenCap  int
	LLMGatewayURL       string // set when the gateway runs as a separate service

	// Range orchestration
	RangeDriver        string // docker | proxmox
	DockerHost         string // e.g. unix:///var/run/docker.sock or tcp://range-host:2376
	DockerAPIVersion   string
	KaliImage          string
	TerminalProxyURL   string // ttyd / guacamole base URL
	RangeSubnetPrefix  string
	RangeSessionMaxMin int
	RangeCPUQuota      float64
	RangeMemoryMB      int64
	ProxmoxURL         string
	ProxmoxTokenID     string
	ProxmoxTokenSecret string
	ProxmoxNode        string

	// SIEM / IDS
	WazuhURL          string
	WazuhUser         string
	WazuhPassword     string
	WazuhVerifyTLS    bool
	SuricataEveFile   string
	IngestPollSeconds int

	// AI red-teaming tooling
	PyRITCommand string
	GarakCommand string

	// Reporting
	PDFRenderer string // chromium | wkhtmltopdf
	PDFBinary   string
	StorageDir  string

	// Attainment
	DirectWeight   float64
	IndirectWeight float64

	// Observability
	MetricsEnabled bool
	LogLevel       string
}

func Load() (*Config, error) {
	// .env is optional; real deployments inject env vars directly.
	_ = godotenv.Load(".env", "../.env", "../../.env")

	c := &Config{
		Env:       str("APP_ENV", "development"),
		HTTPAddr:  str("HTTP_ADDR", ":8080"),
		PublicURL: str("PUBLIC_URL", "http://localhost:3000"),

		DatabaseURL: str("DATABASE_URL", "postgres://cyberrange:cyberrange@localhost:5432/cyberrange?sslmode=disable"),

		RedisURL:      str("REDIS_URL", "redis://localhost:6379/0"),
		RedisPassword: str("REDIS_PASSWORD", ""),

		JWTSecret:          str("JWT_SECRET", ""),
		AccessTokenTTL:     dur("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:    dur("REFRESH_TOKEN_TTL", 7*24*time.Hour),
		CookieDomain:       str("COOKIE_DOMAIN", ""),
		CookieSecure:       boolean("COOKIE_SECURE", false),
		OIDCIssuer:         str("OIDC_ISSUER", ""),
		OIDCClientID:       str("OIDC_CLIENT_ID", ""),
		OIDCClientSecret:   str("OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:    str("OIDC_REDIRECT_URL", ""),
		SAMLMetadataURL:    str("SAML_METADATA_URL", ""),
		AllowLocalPassword: boolean("ALLOW_LOCAL_PASSWORD", true),

		LLMBaseURL:         str("LLM_BASE_URL", "http://localhost:11434"),
		LLMDefaultModel:    str("LLM_DEFAULT_MODEL", "llama3.1:8b"),
		LLMEmbedModel:      str("LLM_EMBED_MODEL", "nomic-embed-text"),
		LLMRequestTimeout:  dur("LLM_REQUEST_TIMEOUT", 120*time.Second),
		LLMAllowPublicIP:   boolean("LLM_ALLOW_PUBLIC_IP", false),
		LLMSessionTokenCap: integer("LLM_SESSION_TOKEN_CAP", 120000),
		LLMGatewayURL:      str("LLM_GATEWAY_URL", ""),

		RangeDriver:        str("RANGE_DRIVER", "docker"),
		DockerHost:         str("DOCKER_HOST", defaultDockerHost()),
		DockerAPIVersion:   str("DOCKER_API_VERSION", "v1.43"),
		KaliImage:          str("RANGE_KALI_IMAGE", "cyberrange/kali-attacker:latest"),
		TerminalProxyURL:   str("TERMINAL_PROXY_URL", ""),
		RangeSubnetPrefix:  str("RANGE_SUBNET_PREFIX", "10.66"),
		RangeSessionMaxMin: integer("RANGE_SESSION_MAX_MINUTES", 180),
		RangeCPUQuota:      float("RANGE_CPU_QUOTA", 2.0),
		RangeMemoryMB:      int64(integer("RANGE_MEMORY_MB", 2048)),
		ProxmoxURL:         str("PROXMOX_URL", ""),
		ProxmoxTokenID:     str("PROXMOX_TOKEN_ID", ""),
		ProxmoxTokenSecret: str("PROXMOX_TOKEN_SECRET", ""),
		ProxmoxNode:        str("PROXMOX_NODE", "pve"),

		WazuhURL:          str("WAZUH_URL", "https://localhost:55000"),
		WazuhUser:         str("WAZUH_USER", "wazuh"),
		WazuhPassword:     str("WAZUH_PASSWORD", ""),
		WazuhVerifyTLS:    boolean("WAZUH_VERIFY_TLS", false),
		SuricataEveFile:   str("SURICATA_EVE_FILE", "/var/log/suricata/eve.json"),
		IngestPollSeconds: integer("INGEST_POLL_SECONDS", 5),

		PyRITCommand: str("PYRIT_COMMAND", "python -m pyrit_runner"),
		GarakCommand: str("GARAK_COMMAND", "garak"),

		PDFRenderer: str("PDF_RENDERER", "chromium"),
		PDFBinary:   str("PDF_BINARY", "chromium"),
		StorageDir:  str("STORAGE_DIR", "./storage"),

		DirectWeight:   float("ATTAINMENT_DIRECT_WEIGHT", 0.8),
		IndirectWeight: float("ATTAINMENT_INDIRECT_WEIGHT", 0.2),

		MetricsEnabled: boolean("METRICS_ENABLED", true),
		LogLevel:       str("LOG_LEVEL", "info"),
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	if c.JWTSecret == "" {
		if c.Env == "production" {
			return fmt.Errorf("JWT_SECRET is required in production")
		}
		// Dev convenience only; production refuses to start without a real secret.
		c.JWTSecret = "dev-only-insecure-secret-change-me"
	}
	if len(c.JWTSecret) < 16 && c.Env == "production" {
		return fmt.Errorf("JWT_SECRET must be at least 16 characters")
	}
	if c.RangeDriver != "docker" && c.RangeDriver != "proxmox" {
		return fmt.Errorf("RANGE_DRIVER must be docker or proxmox, got %q", c.RangeDriver)
	}
	total := c.DirectWeight + c.IndirectWeight
	if total < 0.99 || total > 1.01 {
		return fmt.Errorf("ATTAINMENT_DIRECT_WEIGHT + ATTAINMENT_INDIRECT_WEIGHT must equal 1.0 (got %.2f)", total)
	}
	return nil
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

func defaultDockerHost() string {
	if os.Getenv("OS") == "Windows_NT" {
		return "npipe:////./pipe/docker_engine"
	}
	return "unix:///var/run/docker.sock"
}

func str(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func integer(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func float(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}

func boolean(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

func dur(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return def
}
