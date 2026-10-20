package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPTrustsRealIPOnlyFromPrivatePeer(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/v1/login", nil)
	r.Header.Set("X-Real-IP", "203.0.113.9")

	r.RemoteAddr = "172.18.0.5:41000" // the console's nginx
	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("behind proxy: got %q", got)
	}
	r.RemoteAddr = "198.51.100.7:41000" // straight from the internet
	if got := clientIP(r); got != "198.51.100.7" {
		t.Fatalf("forged header was trusted: got %q", got)
	}
}
