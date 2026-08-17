// Package ingest is the Log Ingest Service. It polls the Wazuh manager REST
// API and tails the Suricata EVE JSON file, normalizes both into the common
// Alert schema, writes new alerts to Postgres, and publishes them to the Redis
// pub/sub channel that powers the live Blue Team console.
package ingest

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"github.com/cyberrange-os/api/internal/config"
	"github.com/cyberrange-os/api/internal/mitre"
	"github.com/cyberrange-os/api/internal/realtime"
	"github.com/cyberrange-os/api/internal/store"
)

type Service struct {
	cfg   *config.Config
	store *store.Store
	hub   *realtime.Hub
	mitre *mitre.Engine
	log   zerolog.Logger
	http  *http.Client

	wazuhToken string
}

func New(cfg *config.Config, st *store.Store, hub *realtime.Hub, mitreEngine *mitre.Engine, log zerolog.Logger) *Service {
	return &Service{
		cfg: cfg, store: st, hub: hub, mitre: mitreEngine, log: log,
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: !cfg.WazuhVerifyTLS},
			},
		},
	}
}

// Start launches both collectors. They run until ctx is cancelled and degrade
// gracefully when a source is unavailable (common in dev without Wazuh).
func (s *Service) Start(ctx context.Context) {
	go s.pollWazuh(ctx)
	go s.tailSuricata(ctx)
	s.log.Info().Msg("log ingest service started")
}

func (s *Service) publish(ctx context.Context, a *store.Alert) {
	s.hub.Publish(ctx, realtime.ChannelAlerts("all"), "alert.new", a)
	if a.SessionID != nil {
		s.hub.Publish(ctx, realtime.ChannelAlerts(a.SessionID.String()), "alert.new", a)
	}
}

// autoTag assigns a MITRE technique from the rule description when absent.
func (s *Service) autoTag(ctx context.Context, a *store.Alert) {
	if a.MitreTechniqueID != nil && *a.MitreTechniqueID != "" {
		return
	}
	text := a.RuleDescription
	if text == "" {
		return
	}
	if tid, conf, err := s.mitre.Tag(ctx, text); err == nil && conf >= 0.4 && tid != "" {
		a.MitreTechniqueID = &tid
	}
}

// ------------------------------------------------------------------ Wazuh

func (s *Service) pollWazuh(ctx context.Context) {
	interval := time.Duration(s.cfg.IngestPollSeconds) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.cfg.WazuhPassword == "" {
				continue // not configured in this environment
			}
			if err := s.fetchWazuhAlerts(ctx); err != nil {
				s.log.Debug().Err(err).Msg("wazuh poll failed")
			}
		}
	}
}

func (s *Service) authWazuh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(s.cfg.WazuhURL, "/")+"/security/user/authenticate", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.cfg.WazuhUser, s.cfg.WazuhPassword)
	res, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("wazuh auth returned %d", res.StatusCode)
	}
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return err
	}
	s.wazuhToken = out.Data.Token
	return nil
}

func (s *Service) fetchWazuhAlerts(ctx context.Context) error {
	if s.wazuhToken == "" {
		if err := s.authWazuh(ctx); err != nil {
			return err
		}
	}
	// The Wazuh Indexer/manager exposes alerts; here we query the manager's
	// most-recent security events endpoint.
	url := strings.TrimRight(s.cfg.WazuhURL, "/") + "/manager/logs?limit=100&sort=-timestamp"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+s.wazuhToken)
	res, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		s.wazuhToken = ""
		return fmt.Errorf("wazuh token expired")
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("wazuh logs returned %d", res.StatusCode)
	}
	var payload struct {
		Data struct {
			AffectedItems []struct {
				Timestamp   string `json:"timestamp"`
				Description string `json:"description"`
				Level       string `json:"level"`
				Tag         string `json:"tag"`
			} `json:"affected_items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return err
	}
	for i, item := range payload.Data.AffectedItems {
		raw, _ := json.Marshal(item)
		extID := fmt.Sprintf("%s-%d", item.Timestamp, i)
		a := store.Alert{
			Source: "wazuh", ExternalID: &extID, RuleID: item.Tag,
			RuleDescription: item.Description, Severity: wazuhSeverity(item.Level),
			RawLog: raw, EventAt: parseTime(item.Timestamp),
		}
		s.autoTag(ctx, &a)
		saved, isNew, err := s.store.InsertAlert(ctx, a)
		if err == nil && isNew {
			s.publish(ctx, saved)
		}
	}
	return nil
}

// ------------------------------------------------------------------ Suricata

func (s *Service) tailSuricata(ctx context.Context) {
	path := s.cfg.SuricataEveFile
	for {
		if ctx.Err() != nil {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			time.Sleep(10 * time.Second) // file not present yet in this env
			continue
		}
		// Start at end of file to stream new events only.
		_, _ = f.Seek(0, io.SeekEnd)
		reader := bufio.NewReader(f)
		s.log.Info().Str("file", path).Msg("tailing Suricata EVE JSON")
		for ctx.Err() == nil {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					time.Sleep(time.Second)
					continue
				}
				break
			}
			s.handleEveLine(ctx, strings.TrimSpace(line))
		}
		f.Close()
	}
}

type eveEvent struct {
	Timestamp string `json:"timestamp"`
	EventType string `json:"event_type"`
	SrcIP     string `json:"src_ip"`
	DestIP    string `json:"dest_ip"`
	FlowID    int64  `json:"flow_id"`
	Alert     struct {
		SignatureID int    `json:"signature_id"`
		Signature   string `json:"signature"`
		Category    string `json:"category"`
		Severity    int    `json:"severity"`
	} `json:"alert"`
}

func (s *Service) handleEveLine(ctx context.Context, line string) {
	if line == "" || !strings.Contains(line, `"event_type":"alert"`) {
		return
	}
	var ev eveEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.EventType != "alert" {
		return
	}
	extID := fmt.Sprintf("%d-%d-%s", ev.FlowID, ev.Alert.SignatureID, ev.Timestamp)
	a := store.Alert{
		Source: "suricata", ExternalID: &extID,
		RuleID:          fmt.Sprintf("%d", ev.Alert.SignatureID),
		RuleDescription: ev.Alert.Signature,
		Severity:        suricataSeverity(ev.Alert.Severity),
		SrcIP:           ev.SrcIP, DstIP: ev.DestIP,
		RawLog:  json.RawMessage(line),
		EventAt: parseTime(ev.Timestamp),
	}
	s.autoTag(ctx, &a)
	saved, isNew, err := s.store.InsertAlert(ctx, a)
	if err == nil && isNew {
		s.publish(ctx, saved)
	}
}

// IngestOne lets other subsystems (e.g. audit dogfooding) inject a
// normalized alert. Exposed for the platform-audit source.
func (s *Service) IngestOne(ctx context.Context, a store.Alert) {
	saved, isNew, err := s.store.InsertAlert(ctx, a)
	if err == nil && isNew {
		s.publish(ctx, saved)
	}
}

var _ = fiber.Map{}

func wazuhSeverity(level string) string {
	switch level {
	case "12", "13", "14", "15":
		return "critical"
	case "9", "10", "11":
		return "high"
	case "6", "7", "8":
		return "medium"
	default:
		return "low"
	}
}

func suricataSeverity(sev int) string {
	switch sev {
	case 1:
		return "high"
	case 2:
		return "medium"
	case 3:
		return "low"
	default:
		return "info"
	}
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999-0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Now().UTC()
}
