package rrdmetrics

import (
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/alexwennerberg/rrd"
	"github.com/go-chi/chi/v5/middleware"
)

type MetricsCollector struct {
	mu sync.RWMutex
	// TODO distinguish int v float
	buffer  map[string]float64
	step    uint
	rrdPath string
	creator *rrd.Creator
	metrics []Metric
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

// TODO
type ComputeMetric struct {
}

// TODO setMinValue, setHeartbeat etc

func NewMetric(name, dsType string) Metric {
	return Metric{
		name:   name,
		dsType: dsType,
		// default values
		heartbeat: 900,
		minValue:  0,
		maxValue:  nil,
	}
}

func (c *MetricsCollector) AddMetric(m Metric) {
	c.metrics = append(c.metrics, m)
}

// or initialize
func (c *MetricsCollector) reset() {
	for _, m := range c.metrics {
		// gauge should not initialize to 0, but rather null/absent values
		if m.dsType == "GAUGE" {
			continue
		}
		c.buffer[m.name] = 0
	}
}

// RegisterMetrics ...
// This has the potential to destroy data when performing a migration. 
// TODO -- cleaner/more sophisticated migration tool?
func (c *MetricsCollector) RegisterMetrics() error {
	if len(c.metrics) == 0 {
		return nil
	}
	c.reset()
	creator := rrd.NewCreator(c.rrdPath, time.Now().Truncate(time.Duration(c.step)*time.Second), c.step)
	var err error
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
		}
		creator.SetSource(c.rrdPath)
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

	// TODO Split out?
	go func() {
		c.start()
	}()
	return nil
}

func (m *MetricsCollector) start() {
	// align ticker to RRD
	wait := time.Duration(m.step)*time.Second - time.Since(time.Now().Truncate(time.Duration(m.step)*time.Second))
	time.Sleep(wait)
	m.storeMetrics()
	ticker := time.NewTicker(time.Duration(m.step) * time.Second)
	for range ticker.C {
		m.storeMetrics()
	}
	// TODO -- on shutdown, write metrics immediately
}

// Update the rrd table with current metrics
func (m MetricsCollector) storeMetrics() {
	// fmt.Printf("storing metrics %v\n at %d", m.buffer, time.Now().Unix())
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

// Generate middleware that updates each request
func (c *MetricsCollector) Middleware(next http.Handler) http.Handler {
	c.AddMetric(NewMetric("count", "ABSOLUTE"))
	c.AddMetric(NewMetric("client_err", "ABSOLUTE"))
	c.AddMetric(NewMetric("server_err", "ABSOLUTE"))
	c.AddMetric(NewMetric("latency", "GAUGE"))
	c.AddMetric(NewMetric("latency_max", "GAUGE"))
	fn := func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		c.mu.Lock()
		defer c.mu.Unlock()
		// running average
		l := c.buffer["latency"]
		l1 := float64(time.Since(start).Milliseconds())
		ct := c.buffer["count"]
		c.buffer["latency"] = ((l * ct) + l1) / (ct + 1)
		if l > c.buffer["latency_max"] {
			c.buffer["latency_max"] = l
		}
		c.buffer["count"]++
		// Count
		if ww.Status()/100 == 4 {
			c.buffer["client_err"]++
		} else if ww.Status()/100 == 5 {
			c.buffer["server_err"]++
		}
	}
	return http.HandlerFunc(fn)
}

// TODO path middleware

// Wrap -> overwrite metric name
// A ds-name must be 1 to 19 characters long in the characters [a-zA-Z0-9_].
// this is limiting

// +4 chars. 15 chars max then. that looks like:
// abcdefghabcdefg_cnt
// get_user_cnt
// get_user_lat
// get_user_latm
// get_user_errs
// get_user_errc

func routeMetric(path string) {
	// strings.Replace(path, "/", "_")
	//
	//
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
