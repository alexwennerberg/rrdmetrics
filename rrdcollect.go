package rrdcollect

type Options struct {
	// Schema mode
	// automigrate -- the schema will automatically add or remove routes on startup to the rrd database (default)
	// fixed -- the schema will not change, even if new routes are added or removed
	SchemaMode string
}

type MetricsCollector struct {
	// worker
	// in memory store of metrics to be written
	store sync.Map
	// the path to the rrdtool db file
	rrdPath string
	// basic step size at which we update metrics (default 300) TODO -> Replace with some rrd underlying thing
	stepSize int
}

// start time will be the most recent
// First update should
// File per endpont -> no schema necessary
// auto metrics (like promhttp) or manual?

// A ds-name must be 1 to 19 characters long in the characters [a-zA-Z0-9_].

// Update the rrd table with current metrics
func (m MetricsCollector) update() {
	// map value shuld be a struct with reqMetrics in it
	// https://stackoverflow.com/a/74746743
}

// TODO -- running median / pXX latency?

// Defaults
//
// DS:watts:GAUGE:5m:0:24000 \
// RRA:AVERAGE:0.5:1s:10d \
// RRA:AVERAGE:0.5:1m:90d \
// RRA:AVERAGE:0.5:1h:18M \
// RRA:AVERAGE:0.5:1d:10y
//
// broken down by route
type reqMetrics struct {
	requests     int
	clientErrors int
	serverErrors int
	totalLatency int // sum of all latencies in ms
}

// NewMetricsCollector creates a new met
func NewMetricsCollector(rrdPath string) MetricsCollector {
	return MetricsCollector
}

// NumGoroutines
