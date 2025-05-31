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
	"sync"
	"syscall"
	"time"

	"github.com/alexwennerberg/rrd"
	"github.com/go-chi/chi/v5/middleware"
)

// MetricsCollector
type MetricsCollector struct {
	mu sync.RWMutex
	// TODO distinguish int v float
	buffer  map[string]float64
	step    uint
	rrdPath string
	creator *rrd.Creator
	metrics []Metric
	gauges  map[string]func() float64
}

type CollectorOption func(*MetricsCollector)

// WithStepSize configures the step size, ie, how frequently in seconds the
// metric collector writes to RRD
func WithStepSize(s uint) CollectorOption {
	return func(m *MetricsCollector) {
		m.step = s
	}
}

// NewCollector creates a new metrics collector
func NewCollector(rrdPath string, options ...CollectorOption) MetricsCollector {
	c := MetricsCollector{
		step:    60,
		rrdPath: rrdPath,
		buffer:  map[string]float64{},
		gauges:  map[string]func() float64{},
	}
	for _, o := range options {
		o(&c)
	}
	return c
}

// Metric is a Metric being tracked by RRDTool
// metrics share the same namespace, so it's up to you to make sure that
// they don't collide
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

// AddMetric adds a metric to the metrics being tracked
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

// Run will syncs the metrics to the RRD database and begin tracking them.
// Renaming an existing metric will lead to it being truncated -- use rrdtool tune
// if this concerns you.
// Metrics with the same name with 'double up'. you're responsible for avoiding
// namespace collisions
func (c *MetricsCollector) Run() error {
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
			fmt.Printf("performing db migration %s. new metric names: %s\n", c.rrdPath, mnames)
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
	// sensible? defaults TODO make configurable
	creator.RRA("AVERAGE", 0.5, "1m", "10d")
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
		m.store()
		fmt.Println("stored metrics before shutdown")
		os.Exit(1)
	}()

	// Align ticker to RRD bucket
	wait := time.Duration(m.step)*time.Second - time.Since(time.Now().Truncate(time.Duration(m.step)*time.Second))
	time.Sleep(wait)
	m.store()

	ticker := time.NewTicker(time.Duration(m.step) * time.Second)
	for range ticker.C {
		m.store()
	}
}

// update the rrd table with current metrics
func (c *MetricsCollector) store() {
	upd := rrd.NewUpdater(c.rrdPath)
	var keys []string
	args := []any{"N"}

	// execute gauges
	for k, v := range c.gauges {
		keys = append(keys, k)
		args = append(args, v())
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.buffer {
		keys = append(keys, k)
		args = append(args, v)
	}
	fmt.Println(c.buffer)
	if len(keys) > 0 {
		upd.SetTemplate(keys...)
		upd.Update(args...)
	}
	c.reset()
}

// AddGaugeMetric will start tracking a metric of a given name whose value is
// fetched by the function.
func (c *MetricsCollector) AddGaugeMetric(name string, gf func() float64) {
	c.AddMetric(NewMetric(name, "GAUGE"))
	c.gauges[name] = gf
}

// HTTPMetric returns a middleware that will start collecting metrics for a
// handler the metric name will be metricName, which must be 14 characters max
// and only consist of alphanumeric, _ or -
func (c *MetricsCollector) HTTPMetric(metricName string) func(http.Handler) http.Handler {
	h := newHTTPMetrics(metricName)
	c.addHTTPMetrics(h)
	if len(metricName) > 14 {
		metricName = stripNonAlpha(metricName[:14])
	}
	return c.httpMetric(func(_ *http.Request) string { return metricName })
}

// build a middleware that actually collects metric
func (c *MetricsCollector) httpMetric(nameFn func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			// TODO remove chi dependency
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			m := newHTTPMetrics(nameFn(r))
			c.mu.Lock()
			defer c.mu.Unlock()
			// running average
			l := c.buffer[m.lat]
			l1 := float64(time.Since(start).Milliseconds())
			ct := c.buffer[m.cnt]
			c.buffer[m.lat] = ((l * ct) + l1) / (ct + 1)
			if l > c.buffer[m.mlat] {
				c.buffer[m.mlat] = l
			}
			c.buffer[m.cnt]++
			if ww.Status()/100 == 4 {
				c.buffer[m.cerr]++
			} else if ww.Status()/100 == 5 {
				c.buffer[m.serr]++
			}
		}
		return http.HandlerFunc(fn)
	}
}

type httpMetrics struct {
	cnt  string
	serr string
	cerr string
	lat  string
	mlat string
}

func newHTTPMetrics(metricName string) httpMetrics {
	var h httpMetrics
	h.cnt = metricName + "_cnt"   // Count
	h.cerr = metricName + "_cerr" // Client Error
	h.serr = metricName + "_serr" // Server Error
	h.lat = metricName + "_lat"   // Latency Average
	h.mlat = metricName + "_mlat" // Max Latency
	return h
}

func (c *MetricsCollector) addHTTPMetrics(hm httpMetrics) {
	c.AddMetric(NewMetric(hm.cnt, "ABSOLUTE"))
	c.AddMetric(NewMetric(hm.cerr, "ABSOLUTE"))
	c.AddMetric(NewMetric(hm.serr, "ABSOLUTE"))
	c.AddMetric(NewMetric(hm.lat, "GAUGE"))
	c.AddMetric(NewMetric(hm.mlat, "GAUGE"))
}
