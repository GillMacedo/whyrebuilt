package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/inventory/internal/store"
)

func TestAddAndList(t *testing.T) {
	h := Handler(store.New())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/add?item=nut&n=2", nil))
	if rec.Code != 200 {
		t.Fatalf("add: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/items", nil))
	if got := strings.TrimSpace(rec.Body.String()); got != `{"nut":2}` {
		t.Fatalf("items: %s", got)
	}
}
