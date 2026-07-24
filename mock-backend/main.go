package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
)

var requestCount atomic.Int64

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "9001"
	}
	name := os.Getenv("NAME")
	if name == "" {
		name = "mock-backend"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy","backend":"` + name + `"}`))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf(
			`{"backend":"%s","path":"%s","method":"%s","count":%d}`,
			name, r.URL.Path, r.Method, n,
		)))
	})

	addr := ":" + port
	log.Printf("%s listening on %s", name, addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
