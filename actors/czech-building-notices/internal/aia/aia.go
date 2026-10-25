// Package aia builds an HTTP client that copes with servers sending an
// incomplete certificate chain (no intermediate). Browsers and macOS fetch
// the missing intermediate from the URL in the leaf's Authority Information
// Access extension; Go on Linux does not. This client does the same and then
// verifies the chain against the system roots and the host name as usual —
// verification is never skipped.
package aia

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type fetcher struct {
	hc    *http.Client
	mu    sync.Mutex
	cache map[string]*x509.Certificate
}

// Client returns an http.Client with AIA chasing and the given timeout.
// Connecting is capped at 10 s: unreachable hosts must not hold a worker for
// the whole request timeout. proxy may be nil.
func Client(timeout time.Duration, proxy *url.URL) *http.Client {
	f := &fetcher{hc: &http.Client{Timeout: 10 * time.Second}, cache: map[string]*x509.Certificate{}}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	tr.TLSHandshakeTimeout = 10 * time.Second
	if proxy != nil {
		tr.Proxy = http.ProxyURL(proxy)
	}
	tr.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true, // replaced by VerifyConnection below, which always verifies
		VerifyConnection:   f.verify,
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}

func (f *fetcher) verify(cs tls.ConnectionState) error {
	if len(cs.PeerCertificates) == 0 {
		return errors.New("aia: no peer certificates")
	}
	leaf := cs.PeerCertificates[0]
	inter := x509.NewCertPool()
	for _, c := range cs.PeerCertificates[1:] {
		inter.AddCert(c)
	}
	opts := x509.VerifyOptions{DNSName: cs.ServerName, Intermediates: inter}
	_, err := leaf.Verify(opts)
	if err == nil {
		return nil
	}
	var ua x509.UnknownAuthorityError
	if !errors.As(err, &ua) {
		return err
	}
	// walk up to 3 missing intermediates
	cur := leaf
	for depth := 0; depth < 3; depth++ {
		next, ferr := f.issuer(cur)
		if ferr != nil {
			return fmt.Errorf("%w (AIA: %v)", err, ferr)
		}
		inter.AddCert(next)
		if _, verr := leaf.Verify(opts); verr == nil {
			return nil
		}
		cur = next
	}
	return err
}

func (f *fetcher) issuer(c *x509.Certificate) (*x509.Certificate, error) {
	for _, u := range c.IssuingCertificateURL {
		f.mu.Lock()
		hit := f.cache[u]
		f.mu.Unlock()
		if hit != nil {
			return hit, nil
		}
		resp, err := f.hc.Get(u) // plain HTTP by design of AIA; integrity comes from chain verification
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			continue
		}
		cert, err := x509.ParseCertificate(b)
		if err != nil {
			if certs, perr := parsePEM(b); perr == nil && len(certs) > 0 {
				cert = certs[0]
			} else {
				continue
			}
		}
		f.mu.Lock()
		f.cache[u] = cert
		f.mu.Unlock()
		return cert, nil
	}
	return nil, errors.New("no usable issuer URL")
}
