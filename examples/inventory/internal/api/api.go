// Package api serves the store over HTTP.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"example.com/inventory/internal/store"
	"example.com/inventory/internal/version"
)

// Handler returns the HTTP handler for a store.
func Handler(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/items", func(w http.ResponseWriter, r *http.Request) {
		out := map[string]int{}
		for _, name := range s.Items() {
			out[name] = s.Count(name)
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(r.URL.Query().Get("n"))
		if err != nil {
			http.Error(w, "bad n", http.StatusBadRequest)
			return
		}
		if err := s.Add(r.URL.Query().Get("item"), n); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
		}
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(version.Version + "\n"))
	})
	return mux
}
