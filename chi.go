package rrdmetrics

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// ChiCollector extends MetricsCollector with some chi-specific stuff
type ChiCollector struct {
	auto bool
	chi  *chi.Mux
	*MetricsCollector
}

// RouteMetrics build a middleware that automatically pulls metrics from routes
func (c *ChiCollector) RouteMetrics() func(http.Handler) http.Handler {
	// MAYBE: figure out some way to 'override' routes?
	// the problem is I don't love putting them in a map
	c.auto = true
	return c.httpMetric(func(r *http.Request) string {
		m := routeMetric(chi.RouteContext(r.Context()).RoutePattern())
		if m == "" {
			return "unknown"
		}
		return m
	})
}

func (c *ChiCollector) Run() error {
	if c.auto {
		// add all the Chi routes as metrics, based on path. unknown as fallback
		routes := map[string]bool{"unknown": true}
		err := chi.Walk(c.chi, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
			routes[routeMetric(route)] = true
			return nil
		})
		if err != nil {
			return err
		}
		for k := range routes {
			h := newHTTPMetrics(k)
			c.addHTTPMetrics(h)
		}
	}
	return c.MetricsCollector.Run()
}

// NewChiCollector extends the metrics collector with some Chi-specific stuff
func (c *MetricsCollector) NewChiCollector(m *chi.Mux) ChiCollector {
	return ChiCollector{
		auto:             false,
		chi:              m,
		MetricsCollector: c,
	}
}

// Tries to build a metric name out of the route
// A ds-name must be 1 to 19 characters long in the characters [a-zA-Z0-9_-].
// we limit to 14 so we can keep the sub metric names
func routeMetric(path string) string {
	if path == "/" {
		path = "root"
	}
	path = strings.Trim(path, "/")
	path = strings.ReplaceAll(path, "/", "_")
	path = strings.ReplaceAll(path, " ", "_")
	path = sanitizeMetricName(path)
	return path
}

func sanitizeMetricName(input string) string {
	var result strings.Builder
	for _, r := range input {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			result.WriteRune(r)
		}
	}
	output := result.String()
	if len(output) > 14 {
		output = output[:14]
	}
	return output
}
