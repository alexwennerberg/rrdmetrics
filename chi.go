package rrdmetrics

import (
	"net/http"
	"strings"
	"unicode"

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
	return c.httpMetric(func(r *http.Request) string { return routeMetric(chi.RouteContext(r.Context()).RoutePattern()) })
}

func (c *ChiCollector) Run() error {
	if c.auto {
		// add all the Chi routes as metrics, based on path
		routes := map[string]bool{}
		chi.Walk(c.chi, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
			routes[route] = true
			return nil
		})
		for k, _ := range routes {
			h := newHTTPMetrics(routeMetric(k))
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
	path = stripNonAlpha(path)
	if len(path) > 14 {
		path = path[:14]
	}
	return path
}

func stripNonAlpha(input string) string {
	var result []rune
	for _, r := range input {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result = append(result, r)
		}
	}
	return string(result)
}
