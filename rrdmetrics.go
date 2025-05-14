package rrdmetrics

import (
	"sync"
	"time"

	"github.com/ziutek/rrd"
)

//step -> should align with what we write from our app. with tick, we prob don't need to worry too much about interpolation

// on shutdown -> write data immediately. this is a case in which interpolation
// will be used. otherwise write when tick is aligned with bucket window

// TODO: rrdcached?

type Options struct {
}

type MetricsCollector struct {
	// worker
	// in memory store of metrics to be written
	store   sync.Map
	rrdPath string
	// basic step size at which we update metrics (default 300) TODO -> Replace with some rrd underlying thing?
	stepSize int
}

// start time will be the most recent
// First update should
// File per endpont -> no schema necessary
// auto metrics (like promhttp) or manual?

// A ds-name must be 1 to 19 characters long in the characters [a-zA-Z0-9_].
// this is limiting

// Update the rrd table with current metrics
func (m MetricsCollector) update() {
	// map value shuld be a struct with reqMetrics in it
	// https://stackoverflow.com/a/74746743
}

// TODO -- running median / pXX latency?

// Defaults ?
//
// DS:watts:GAUGE:5m:0:24000 \
// RRA:AVERAGE:0.5:1s:10d \
// RRA:AVERAGE:0.5:1m:90d \
// RRA:AVERAGE:0.5:1h:18M \
// RRA:AVERAGE:0.5:1d:10y
//
// broken down by route
type reqMetrics struct {
	// These should all be counters. stored as rps
	requests     int
	clientErrors int
	serverErrors int
	// I think this should be a gauge
	totalLatency int // sum of all latencies in ms
}

// NewMetricsCollector creates a new met
func NewMetricsCollector(filename string) (MetricsCollector, error) {
	// TODO parameterize
	var step uint = 30 // seconds
	c := rrd.NewCreator(filename, time.Now().Truncate(time.Duration(step)*time.Second), step)
	// c. DS...
	// c. RRA...
	err := c.Create(false)
	m := MetricsCollector{}
	return m, err
}

// NumGoroutines
