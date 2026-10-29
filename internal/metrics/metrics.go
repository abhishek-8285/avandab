// Package metrics provides a dependency-free Prometheus text-exposition
// endpoint with golden-signal request metrics. Scraping /metrics gives
// request rates, error rates, and latency histograms per route — the
// observability baseline that was missing until now.
package metrics

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// buckets are the latency histogram edges in seconds.
var buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type seriesKey struct {
	name   string // metric family
	labels string // pre-rendered label set
}

type histogram struct {
	bucketCounts [12]uint64 // len(buckets) cumulative-or-lower counts
	count        uint64
	sum          float64
}

// Registry accumulates counters and histograms. Zero-configuration: use the
// package-level Default registry.
type Registry struct {
	mu      sync.Mutex
	series  map[seriesKey]*histogram
	started time.Time
}

// Default is the process-wide registry wired into Middleware and Handler.
var Default = NewRegistry()

func NewRegistry() *Registry {
	return &Registry{series: make(map[seriesKey]*histogram), started: time.Now()}
}

// recordRequest adds one observation for method/route/status.
func (rg *Registry) recordRequest(method, route string, status int, d time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	lbl := fmt.Sprintf("method=%q,route=%q,status=%d", method, route, status)
	reqKey := seriesKey{name: "http_requests_total", labels: lbl}
	durKey := seriesKey{name: "http_request_duration_seconds", labels: fmt.Sprintf("method=%q,route=%q", method, route)}

	rg.mu.Lock()
	defer rg.mu.Unlock()
	if c, ok := rg.series[reqKey]; ok {
		c.count++
	} else {
		rg.series[reqKey] = &histogram{count: 1}
	}

	h, ok := rg.series[durKey]
	if !ok {
		h = &histogram{}
		rg.series[durKey] = h
	}
	secs := d.Seconds()
	h.count++
	h.sum += secs
	for i, b := range buckets {
		if secs <= b {
			h.bucketCounts[i]++
		}
	}
}

// Render produces Prometheus text format version 0.0.4.
func (rg *Registry) Render() string {
	rg.mu.Lock()
	defer rg.mu.Unlock()

	var sb strings.Builder
	names := make([]seriesKey, 0, len(rg.series))
	for k := range rg.series {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i].name != names[j].name {
			return names[i].name < names[j].name
		}
		return names[i].labels < names[j].labels
	})

	emittedType := map[string]bool{}
	for _, k := range names {
		h := rg.series[k]
		switch k.name {
		case "http_requests_total":
			if !emittedType[k.name] {
				sb.WriteString("# HELP http_requests_total Total HTTP requests.\n# TYPE http_requests_total counter\n")
				emittedType[k.name] = true
			}
			fmt.Fprintf(&sb, "http_requests_total{%s} %d\n", k.labels, h.count)
		case "http_request_duration_seconds":
			if !emittedType[k.name] {
				sb.WriteString("# HELP http_request_duration_seconds Request latency.\n# TYPE http_request_duration_seconds histogram\n")
				emittedType[k.name] = true
			}
			base := strings.TrimSuffix(k.labels, ")")
			for i, b := range buckets {
				fmt.Fprintf(&sb, "http_request_duration_seconds_bucket{%s,le=\"%g\"} %d\n", base, b, h.bucketCounts[i])
			}
			fmt.Fprintf(&sb, "http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", base, h.count)
			fmt.Fprintf(&sb, "http_request_duration_seconds_sum{%s} %f\n", base, h.sum)
			fmt.Fprintf(&sb, "http_request_duration_seconds_count{%s} %d\n", base, h.count)
		}
	}

	fmt.Fprintf(&sb, "# HELP app_uptime_seconds Seconds since process start.\n# TYPE app_uptime_seconds gauge\napp_uptime_seconds %f\n",
		time.Since(rg.started).Seconds())
	return sb.String()
}

// statusWriter captures the response code written by downstream handlers.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Middleware records per-route request counts and latencies on the Default
// registry. Mount once, globally, before the router groups.
func Middleware(next http.Handler) http.Handler {
	return Default.MiddlewareFor()(next)
}

// MiddlewareFor is like Middleware but records into rg — lets tests and
// future multi-scrape setups use isolated registries.
func (rg *Registry) MiddlewareFor() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			route := chi.RouteContext(r.Context()).RoutePattern()
			rg.recordRequest(r.Method, route, sw.status, time.Since(start))
		})
	}
}

// Handler serves the Prometheus exposition at GET /metrics.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(Default.Render()))
	})
}

// Guard protects the exposition endpoint. Loopback scrapes stay open (local
// node exporter / sidecar); remote scrapes require the bearer token. An empty
// token means remote scrapes are refused — never exposed unauthenticated.
func Guard(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isLoopback(r.RemoteAddr) {
			next.ServeHTTP(w, r)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
