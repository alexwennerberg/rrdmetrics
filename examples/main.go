package main

import (
	"fmt"
	"math/rand"
	"net/http"

	"github.com/alexwennerberg/rrdmetrics"
	"github.com/go-chi/chi/v5"
)

func pong(w http.ResponseWriter, r *http.Request) {
	fmt.Println("pong")
	w.Write([]byte("OK"))
}

func randGauge() float64 {
	return rand.Float64()
}

// func stdlib() {
// 	c := rrdmetrics.NewCollector("test.rrd")
// 	c.AddGaugeMetric("mygauge", randGauge)
// 	mux := http.NewServeMux()
// 	mux.Handle("/", c.HTTPMetric("home")(http.HandlerFunc(pong)))

// 	err := c.Track()
// 	fmt.Println("Tracking metrics")
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	err = http.ListenAndServe(":8080", c.HTTPMetric("total")(mux))
// 	log.Fatal(err)
// }

func main() {
	r := chi.NewRouter()
	a := rrdmetrics.NewCollector("test.rrd", rrdmetrics.WithStepSize(5))
	c := a.NewChiCollector(r)
	r.Use(c.RouteMetrics())
	r.Use(c.HTTPMetric("total"))
	r.HandleFunc("/", pong)
	r.HandleFunc("/ping", pong)
	r.HandleFunc("/pang", pong)
	err := c.Run()
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("starting server")
	http.ListenAndServe(":8080", r)
}
