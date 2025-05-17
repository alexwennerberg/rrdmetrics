package rrdmetrics

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/ziutek/rrd"
)

type MetricsCollector struct {
	// worker
	// in memory store of metrics to be written
	mu      sync.RWMutex
	buffer  map[string]int
	rrdPath string
	// rrd metric name -> value
}

// A ds-name must be 1 to 19 characters long in the characters [a-zA-Z0-9_].
// this is limiting

// Update the rrd table with current metrics
func (m MetricsCollector) storeMetrics() {
	fmt.Printf("storing metrics %v\n at %d", m.buffer, time.Now().Unix())
	m.mu.Lock()
	defer m.mu.Unlock()
	upd := rrd.NewUpdater(m.rrdPath)
	var keys []string
	args := []any{"N"}
	for k,v := range m.buffer {
		keys = append(keys, k)
		args = append(args, v)
		// TODO I want to null latency
		m.buffer[k] = 0
	}
	if len(keys) > 0 {
		upd.SetTemplate(keys...)
		err := upd.Update(args...)
		if err != nil {
			// TODO squash errs
			fmt.Println(err)
		}
	}
}

// Generate middleware that updates each request
func (m MetricsCollector) Middleware(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		m.mu.Lock()
		defer m.mu.Unlock()
		m.buffer["count"]++
		// Count
		if ww.Status()/100 == 4 {
			m.buffer["client_err"]++
		} else if ww.Status()/100 == 5 {
			m.buffer["server_err"]++
		}
		// TODO make a running avg
		m.buffer["latency"] = int(time.Since(start).Milliseconds())
	}
	return http.HandlerFunc(fn)
}

// TODO path middleware

// Wrap -> overwrite metric name
// names have to be 19 chars max ??, limited character set

// +4 chars. 15 chars max then. that looks like:
// abcdefghabcdefg_cnt
// get_user_cnt
// get_user_lat
// get_user_ers
// get_user_ercokkj:w

// updates an existing db with the new schema TODO
func dbMigrate() {
}

// NewMetricsCollector creates a new metrics collector, which will write every
// 60 seconds (not configurable right now) with some reasonable rra defaults
func NewCollector(filename string) (MetricsCollector, error) {
	var step uint = 60
	c := rrd.NewCreator(filename, time.Now().Truncate(time.Duration(step)*time.Second), step)
	c.DS("count", "ABSOLUTE", 900, 0, "U")
	c.DS("client_err", "ABSOLUTE", 900, 0, "U")
	c.DS("server_err", "ABSOLUTE", 900, 0, "U")
	c.DS("latency", "GAUGE", 900, 0, "U")
	c.RRA("AVERAGE", 0.5, "1m", "90d")
	c.RRA("AVERAGE", 0.5, "1h", "18M")
	c.RRA("AVERAGE", 0.5, "1d", "10y")

	err := c.Create(true) // TODO -- truncates currently
	m := MetricsCollector{
		buffer: map[string]int{
			"count":      0,
			"client_err": 0,
			"server_err": 0,
		},
		rrdPath: filename,
	}

	go func() {
		// align ticker 
		wait := time.Duration(step) * time.Second - time.Since(time.Now().Truncate(time.Duration(step) * time.Second))
		time.Sleep(wait)
		m.storeMetrics()
		ticker := time.NewTicker(time.Duration(step) * time.Second)
		for range ticker.C {
			m.storeMetrics()
		}
		// TODO -- on shutdown, write metrics immediately
	}()
	return m, err
}
