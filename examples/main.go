package main

import (
	"log"
	"net/http"
	"fmt"

	"git.sr.ht/~aw/rrdmetrics"
)

func pong(w http.ResponseWriter, r *http.Request) {
	fmt.Println("pong")
	w.Write([]byte("OK"))
}

func main() {
	c, err := rrdmetrics.NewCollector("test.rrd")
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", c.Middleware(http.HandlerFunc(pong)))
	err = http.ListenAndServe(":8080", mux)
	log.Fatal(err)
}
