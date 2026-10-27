package main

import (
	"fmt"
	"httpServer/internal/http1"
	"log"
	"net/http"
)

const addr = ":8080"

func main() {
	http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		body := "hello"
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})

	fmt.Printf("HTTP server listening on %s...\n", addr)
	if err := http1.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
