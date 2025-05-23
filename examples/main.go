package main

import (
	"fmt"
	"log"
	"net/http"

	"git.sr.ht/~aw/rrdmetrics"
)

func pong(w http.ResponseWriter, r *http.Request) {
	fmt.Println("pong")
	w.Write([]byte("OK"))
}

func main() {
	c := rrdmetrics.NewCollector("test.rrd", 1)
	mux := http.NewServeMux()
	mux.Handle("/", c.Middleware(http.HandlerFunc(pong)))

	err := c.RegisterMetrics()
	fmt.Println("Registered metrics")
	if err != nil {
		log.Fatal(err)
	}
	err = http.ListenAndServe(":8080", mux)
	log.Fatal(err)
}
