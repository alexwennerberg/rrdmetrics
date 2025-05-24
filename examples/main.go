package main

import (
	"fmt"
	"log"
	"net/http"
	"math/rand"

	"git.sr.ht/~aw/rrdmetrics"
)

func pong(w http.ResponseWriter, r *http.Request) {
	fmt.Println("pong")
	w.Write([]byte("OK"))
}

func randGauge() float64 {
	return rand.Float64()
}

func main() {
	c := rrdmetrics.NewCollector("test.rrd", 1)
	c.AddGaugeMetric("mygauge", randGauge)
	mux := http.NewServeMux()
	mux.Handle("/", c.HTTPMetric("home", http.HandlerFunc(pong)))

	err := c.Track()
	fmt.Println("Tracking metrics")
	if err != nil {
		log.Fatal(err)
	}
	err = http.ListenAndServe(":8080", mux)
	log.Fatal(err)
}
