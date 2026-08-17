// Package metrics exposes a tiny Prometheus-style text endpoint without
// pulling the full client library: request counts, in-flight sessions, and
// latency buckets are enough for the Grafana dashboards described in the spec.
package metrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

var (
	mu           sync.Mutex
	reqTotal     = map[string]int64{}
	reqDuration  = map[string]float64{}
	gauges       = map[string]float64{}
	latencyBucket = map[float64]int64{0.05: 0, 0.1: 0, 0.3: 0, 1: 0, 3: 0}
	latencyCount int64
	latencySum   float64
)

func Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		elapsed := time.Since(start).Seconds()
		key := c.Method() + " " + c.Route().Path
		mu.Lock()
		reqTotal[key]++
		reqDuration[key] += elapsed
		latencyCount++
		latencySum += elapsed
		for b := range latencyBucket {
			if elapsed <= b {
				latencyBucket[b]++
			}
		}
		mu.Unlock()
		return err
	}
}

func SetGauge(name string, v float64) {
	mu.Lock()
	gauges[name] = v
	mu.Unlock()
}

func IncGauge(name string, delta float64) {
	mu.Lock()
	gauges[name] += delta
	mu.Unlock()
}

func Handler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		mu.Lock()
		defer mu.Unlock()
		var b strings.Builder
		b.WriteString("# HELP cyberrange_requests_total Total HTTP requests by route\n")
		b.WriteString("# TYPE cyberrange_requests_total counter\n")
		for k, v := range reqTotal {
			parts := strings.SplitN(k, " ", 2)
			b.WriteString(fmt.Sprintf("cyberrange_requests_total{method=%q,route=%q} %d\n", parts[0], parts[1], v))
		}
		b.WriteString("# HELP cyberrange_request_latency_seconds API latency histogram\n")
		b.WriteString("# TYPE cyberrange_request_latency_seconds histogram\n")
		bounds := []float64{}
		for k := range latencyBucket {
			bounds = append(bounds, k)
		}
		sort.Float64s(bounds)
		for _, bound := range bounds {
			b.WriteString(fmt.Sprintf("cyberrange_request_latency_seconds_bucket{le=\"%g\"} %d\n", bound, latencyBucket[bound]))
		}
		b.WriteString(fmt.Sprintf("cyberrange_request_latency_seconds_bucket{le=\"+Inf\"} %d\n", latencyCount))
		b.WriteString(fmt.Sprintf("cyberrange_request_latency_seconds_sum %g\n", latencySum))
		b.WriteString(fmt.Sprintf("cyberrange_request_latency_seconds_count %d\n", latencyCount))
		for name, v := range gauges {
			b.WriteString(fmt.Sprintf("# TYPE cyberrange_%s gauge\ncyberrange_%s %g\n", name, name, v))
		}
		c.Set("Content-Type", "text/plain; version=0.0.4")
		return c.SendString(b.String())
	}
}
