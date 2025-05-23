// rrdmetrics uses RRDTool as a backend to track application metrics, such as
// for a running web server
package rrdmetrics

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/alexwennerberg/rrd"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/v5/middleware"
)

// Logger TODO figure out ideal
type Logger interface {
	Info(msg string, args ...any)
}

type MetricsCollector struct {
	mu sync.RWMutex
	// TODO distinguish int v float
	buffer  map[string]float64
	step    uint
	rrdPath string
	creator *rrd.Creator
	metrics []Metric
	log     Logger
}

// NewCollector creates a new metrics collector, which will collect every
// step seconds. 60 is a good default
func NewCollector(rrdPath string, step uint) MetricsCollector {
	return MetricsCollector{
		step:    step,
		rrdPath: rrdPath,
		buffer:  map[string]float64{},
	}
}

func (c *MetricsCollector) SetLogger(l Logger) {
	c.log = l
}

// a Metric being tracked by RRDTool
type Metric struct {
	name string
	// GAUGE (float) | COUNTER (int) | DERIVE (int) | DCOUNTER (float) | DDERIVE (float) | ABSOLUTE (int?). COMPUTE not yet supported
	// In practice, GAUGE and ABSOLUTE
	dsType string
	// less used
	heartbeat int
	minValue  int
	maxValue  *int
}

type MetricOption func(*Metric)

func WithHeartbeat(h int) MetricOption {
	return func(m *Metric) {
		m.heartbeat = h
	}
}

func WithMinValue(mn int) MetricOption {
	return func(m *Metric) {
		m.minValue = mn
	}
}

func WithMaxValue(mx int) MetricOption {
	return func(m *Metric) {
		m.maxValue = &mx
	}
}

func NewMetric(name, dsType string, options ...MetricOption) Metric {
	m := Metric{
		name:      name,
		dsType:    dsType,
		heartbeat: 900,
		minValue:  0,
		maxValue:  nil,
	}
	for _, o := range options {
		o(&m)
	}
	return m
}

func (c *MetricsCollector) AddMetric(m Metric) {
	c.metrics = append(c.metrics, m)
}

func (c *MetricsCollector) reset() {
	for _, m := range c.metrics {
		// GAUGE should not initialize to 0, but rather null/absent
		if m.dsType == "GAUGE" {
			delete(c.buffer, m.name)
		}
		c.buffer[m.name] = 0
	}
}

// Track syncs the metrics to the RRD database and begins tracking them.
// metrics will be stored in a buffer and written every `step` seconds
// renaming an existing metric will lead to it being truncated -- use rrdtool tune
// if this concerns you
// Metrics with the same name with 'double up'. you're responsible for avoiding
// namespace collisions
func (c *MetricsCollector) Track() error {
	if len(c.metrics) == 0 {
		return nil
	}
	c.reset()
	creator := rrd.NewCreator(c.rrdPath, time.Now().Truncate(time.Duration(c.step)*time.Second), c.step)
	var err error
	// TODO Split out?
	go func() {
		c.start()
	}()

	// perform DB migration if we have added or removed any metrics
	// how's performance on this?
	if _, err := os.Stat(c.rrdPath); os.IsNotExist(err) {
	} else {
		info, err := rrd.Info(c.rrdPath)
		if err != nil {
			return fmt.Errorf("could not get info for %s: %w", c.rrdPath, err)
		}
		var mnames []string
		for _, metric := range c.metrics {
			mnames = append(mnames, metric.name)
		}
		var rkeys []string = slices.Collect(maps.Keys(info["ds.index"].(map[string]any)))
		sort.Strings(rkeys)
		sort.Strings(mnames)
		if !slices.Equal(rkeys, mnames) {
			// TODO logging
			fmt.Println("performing db migration %s", c.rrdPath)
			creator.SetSource(c.rrdPath)
		} else {
			// Do nothing
			return nil
		}
	}

	for _, m := range c.metrics {
		maximum := "U"
		if m.maxValue != nil {
			maximum = strconv.Itoa(*m.maxValue)
		}
		creator.DS(m.name, m.dsType, m.heartbeat, m.minValue, maximum)
	}
	// sensible defaults TODO make configurable
	creator.RRA("AVERAGE", 0.5, "1m", "90d")
	creator.RRA("AVERAGE", 0.5, "1h", "18M")
	creator.RRA("AVERAGE", 0.5, "1d", "10y")
	err = creator.Create(true)
	if err != nil {
		return fmt.Errorf("trouble creating db file %s: %w", c.rrdPath, err)
	}

	return nil
}

func (m *MetricsCollector) start() {
	// On shutodwn, write metrics immediately
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		m.storeMetrics()
		fmt.Println("stored metrics before shutdown")
	}()

	// Align ticker to RRD bucket
	wait := time.Duration(m.step)*time.Second - time.Since(time.Now().Truncate(time.Duration(m.step)*time.Second))
	time.Sleep(wait)
	m.storeMetrics()

	ticker := time.NewTicker(time.Duration(m.step) * time.Second)
	for range ticker.C {
		m.storeMetrics()
	}
}

// Update the rrd table with current metrics
func (m MetricsCollector) storeMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()
	upd := rrd.NewUpdater(m.rrdPath)
	var keys []string
	args := []any{"N"}
	for k, v := range m.buffer {
		keys = append(keys, k)
		args = append(args, v)
	}
	if len(keys) > 0 {
		upd.SetTemplate(keys...)
		upd.Update(args...)
	}
	m.reset()
}

// TODO?
// https://stackoverflow.com/questions/23166411/wrapper-for-arbitrary-function-in-go
func (c *MetricsCollector) DBMetric() {}

// Call this function regularly
func (c *MetricsCollector) AddGaugeMetric(gf func() float64) {}

// HTTPMetric Generates request middleware that updates each request
// metricName must be 14 characters or shorter, based on rrd limitations
func (c *MetricsCollector) HTTPMetric(metricName string, next http.Handler) http.Handler {
	if len(metricName) > 14 {
		metricName = metricName[:14]
	}
	cnt := metricName + "_cnt"   // Count
	cerr := metricName + "_cerr" // Client Error
	serr := metricName + "_serr" // Server Error
	lat := metricName + "_lat"   // Latency Average
	mlat := metricName + "_mlat" // Max Latency
	c.AddMetric(NewMetric(cnt, "ABSOLUTE"))
	c.AddMetric(NewMetric(cerr, "ABSOLUTE"))
	c.AddMetric(NewMetric(serr, "ABSOLUTE"))
	c.AddMetric(NewMetric(lat, "GAUGE"))
	c.AddMetric(NewMetric(mlat, "GAUGE"))
	fn := func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		c.mu.Lock()
		defer c.mu.Unlock()
		// running average
		l := c.buffer[lat]
		l1 := float64(time.Since(start).Milliseconds())
		ct := c.buffer[cnt]
		c.buffer[lat] = ((l * ct) + l1) / (ct + 1)
		if l > c.buffer[mlat] {
			c.buffer[mlat] = l
		}
		c.buffer[cnt]++
		// Count
		if ww.Status()/100 == 4 {
			c.buffer[cerr]++
		} else if ww.Status()/100 == 5 {
			c.buffer[serr]++
		}
	}
	return http.HandlerFunc(fn)
}

// ChiMetrics wraps all routes associated with a chi router in
func (c *MetricsCollector) ChiMetrics(r chi.Router) {
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
		path = path[:15]
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

// .... muxer or osmethign
func GraphServer() {
}
