package rrdmetrics

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/ziutek/rrd"
)

//step -> should align with what we write from our app. with tick, we prob don't need to worry too much about interpolation

// on shutdown -> write data immediately. this is a case in which interpolation
// will be used. otherwise write when tick is aligned with bucket window

type MetricsCollector struct {
	// worker
	// in memory store of metrics to be written
	mu      sync.RWMutex
	buffer  map[string]int
	rrdPath string
	// rrd metric name -> value
}

// start time will be the most recent
// First update should
// File per endpont -> no schema necessary
// auto metrics (like promhttp) or manual?

// A ds-name must be 1 to 19 characters long in the characters [a-zA-Z0-9_].
// this is limiting

// Update the rrd table with current metrics
func (m MetricsCollector) storeMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()
	upd := rrd.NewUpdater(m.rrdPath)
	keys := slices.Sorted(maps.Keys(m.buffer))
	values := slices.Sorted(maps.Values(m.buffer))
	upd.SetTemplate(keys...)
	var anySlice []interface{}
    for _, v := range values{
        anySlice = append(anySlice, v)
    }
	err := upd.Update(anySlice...)
	if err != nil {
		// TODO squash errs
		fmt.Println(err)
	}
	clear(m.buffer)
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

// +4 chars. 15 chars max then
// get_user_cnt
// get_user_lat
// get_user_ers
// get_user_erc

// updates an existing db with the new schema TODO
func dbMigrate() {
}

// NewMetricsCollector creates a new metrics collector, which will write every
// 60 seconds (not configurable right now) with some reasonable rra defaults
func NewCollector(filename string) (MetricsCollector, error) {
	var step uint = 60
	c := rrd.NewCreator(filename, time.Now().Truncate(time.Duration(step)*time.Second), step)
	c.DS("count", "COUNTER", 900, 0, "U")
	c.DS("client_err", "COUNTER", 900, 0, "U")
	c.DS("server_err", "COUNTER", 900, 0, "U")
	c.DS("latency", "GAUGE", 900, 0, "U")
	c.RRA("AVERAGE", 0.5, "1m", "90d")
	c.RRA("AVERAGE", 0.5, "1h", "18M")
	c.RRA("AVERAGE", 0.5, "1d", "10y")

	err := c.Create(false)
	m := MetricsCollector{}

	go func() {
		// align ticker
		wait := time.Since(time.Now().Truncate(time.Duration(step) * time.Second))
		time.Sleep(wait)

		ticker := time.NewTicker(time.Duration(step) * time.Second)
		for range ticker.C {
			m.storeMetrics()
		}
	}()
	return m, err
}
