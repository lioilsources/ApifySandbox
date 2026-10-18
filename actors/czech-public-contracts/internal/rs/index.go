// Copied from github.com/lioilsources/leadscout internal/rs (LeadsRAG); keep in sync.
package rs

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"time"
)

// IndexURL lists every monthly and daily dump with its hash and size.
const IndexURL = "https://data.smlouvy.gov.cz/"

// Dump is one entry of the open-data index.
type Dump struct {
	Year      int    `xml:"rok"`
	Month     int    `xml:"mesic"`
	Day       int    `xml:"den"` // 0 for monthly dumps
	SHA1      string `xml:"hashDumpu"`
	Size      int64  `xml:"velikostDumpu"`
	Generated string `xml:"casGenerovani"`
	Complete  int    `xml:"dokoncenyMesic"`
	URL       string `xml:"odkaz"`
}

func (d Dump) Name() string  { return path.Base(d.URL) }
func (d Dump) Monthly() bool { return d.Day == 0 }
func (d Dump) Date() time.Time {
	return time.Date(d.Year, time.Month(d.Month), max(d.Day, 1), 0, 0, 0, 0, time.UTC)
}

func FetchIndex(ctx context.Context, c *http.Client) ([]Dump, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, IndexURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("index: HTTP %d", resp.StatusCode)
	}
	var idx struct {
		Dumps []Dump `xml:"dump"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}
	return idx.Dumps, nil
}

// Download saves the dump to dst and verifies size and SHA-1 from the index.
func Download(ctx context.Context, c *http.Client, d Dump, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", d.Name(), resp.StatusCode)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha1.New()
	n, err := io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%s: %w", d.Name(), err)
	}
	if d.Size > 0 && n != d.Size {
		os.Remove(tmp)
		return fmt.Errorf("%s: size %d, index says %d", d.Name(), n, d.Size)
	}
	if sum := hex.EncodeToString(h.Sum(nil)); d.SHA1 != "" && sum != d.SHA1 {
		os.Remove(tmp)
		return fmt.Errorf("%s: sha1 %s, index says %s", d.Name(), sum, d.SHA1)
	}
	return os.Rename(tmp, dst)
}
