package aia

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
)

func parsePEM(b []byte) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	for {
		var blk *pem.Block
		blk, b = pem.Decode(b)
		if blk == nil {
			break
		}
		if c, err := x509.ParseCertificate(blk.Bytes); err == nil {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no PEM certificates")
	}
	return out, nil
}
