// Command inventory serves item counts over HTTP.
package main

import (
	"flag"
	"log"
	"net/http"

	"example.com/inventory/internal/api"
	"example.com/inventory/internal/store"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "listen address")
	flag.Parse()
	log.Fatal(http.ListenAndServe(*addr, api.Handler(store.New())))
}
