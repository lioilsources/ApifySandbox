//go:build online

package aia

import (
	"testing"
	"time"
)

// edeska.brno.cz sent no intermediate on 2026-10-25; must work, and a
// wrong host name must still fail.
func TestIncompleteChain(t *testing.T) {
	hc := Client(30*time.Second, nil)
	resp, err := hc.Head("https://edeska.brno.cz/eDeska/opendata")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := hc.Get("https://wrong.host.badssl.com/"); err == nil {
		t.Error("wrong host accepted")
	}
	if _, err := hc.Get("https://self-signed.badssl.com/"); err == nil {
		t.Error("self-signed accepted")
	}
	if _, err := hc.Get("https://expired.badssl.com/"); err == nil {
		t.Error("expired accepted")
	}
}
