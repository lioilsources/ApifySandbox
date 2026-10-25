// Package actor (copied from actors/ruian-parcel-mcp, plus named stores) — originally
// from actors/czech-public-contracts, plus SetRecord and
// Buffered) is the small slice of the Apify Actor runtime a Go binary needs:
// read INPUT, push dataset items and detect Standby mode.
//
// Apify has no Go SDK. On the platform (APIFY_IS_AT_HOME=1) storage goes
// through the REST API with the run's token; locally it uses the same
// ./storage layout as `apify run`, so the binary works offline.
package actor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type Env struct {
	AtHome    bool
	Token     string
	APIBase   string
	DatasetID string
	KVStoreID string
	InputKey  string
	Storage   string // local storage dir
	Standby   bool
	Port      int
}

func getenv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func LoadEnv() Env {
	e := Env{
		AtHome:    os.Getenv("APIFY_IS_AT_HOME") == "1",
		Token:     os.Getenv("APIFY_TOKEN"),
		APIBase:   getenv("APIFY_API_PUBLIC_BASE_URL", "APIFY_API_BASE_URL"),
		DatasetID: getenv("ACTOR_DEFAULT_DATASET_ID", "APIFY_DEFAULT_DATASET_ID"),
		KVStoreID: getenv("ACTOR_DEFAULT_KEY_VALUE_STORE_ID", "APIFY_DEFAULT_KEY_VALUE_STORE_ID"),
		InputKey:  getenv("ACTOR_INPUT_KEY", "APIFY_INPUT_KEY"),
		Storage:   getenv("APIFY_LOCAL_STORAGE_DIR", "CRAWLEE_STORAGE_DIR"),
		Standby:   os.Getenv("APIFY_META_ORIGIN") == "STANDBY",
	}
	if e.APIBase == "" {
		e.APIBase = "https://api.apify.com"
	}
	if e.InputKey == "" {
		e.InputKey = "INPUT"
	}
	if e.Storage == "" {
		e.Storage = "storage"
	}
	if e.DatasetID == "" {
		e.DatasetID = "default"
	}
	if e.KVStoreID == "" {
		e.KVStoreID = "default"
	}
	e.Port, _ = strconv.Atoi(getenv("ACTOR_STANDBY_PORT", "ACTOR_WEB_SERVER_PORT"))
	if e.Port == 0 {
		e.Port = 4321
	}
	return e
}

type Actor struct {
	Env  Env
	http *http.Client

	mu      sync.Mutex
	buf     []any
	written int
}

func New() *Actor {
	return &Actor{Env: LoadEnv(), http: &http.Client{Timeout: time.Minute}}
}

func (a *Actor) api(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, a.Env.APIBase+"/v2"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.Env.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return a.http.Do(req)
}

// Input decodes the run input into v. A missing input leaves v untouched.
func (a *Actor) Input(ctx context.Context, v any) error {
	if !a.Env.AtHome {
		b, err := os.ReadFile(filepath.Join(a.Env.Storage, "key_value_stores", a.Env.KVStoreID, a.Env.InputKey+".json"))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return json.Unmarshal(b, v)
	}
	resp, err := a.api(ctx, http.MethodGet, "/key-value-stores/"+a.Env.KVStoreID+"/records/"+a.Env.InputKey, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("input: HTTP %d: %s", resp.StatusCode, b)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Push buffers an item; batches of 100 are flushed to the dataset.
func (a *Actor) Push(ctx context.Context, item any) error {
	a.mu.Lock()
	a.buf = append(a.buf, item)
	full := len(a.buf) >= 100
	a.mu.Unlock()
	if full {
		return a.Flush(ctx)
	}
	return nil
}

// Flush writes buffered items. Call it before exit.
func (a *Actor) Flush(ctx context.Context) error {
	a.mu.Lock()
	items := a.buf
	a.buf = nil
	a.mu.Unlock()
	if len(items) == 0 {
		return nil
	}
	if !a.Env.AtHome {
		dir := filepath.Join(a.Env.Storage, "datasets", a.Env.DatasetID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		for _, it := range items {
			a.written++
			b, _ := json.MarshalIndent(it, "", "  ")
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%09d.json", a.written)), b, 0o644); err != nil {
				return err
			}
		}
		return nil
	}
	b, err := json.Marshal(items)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		resp, err := a.api(ctx, http.MethodPost, "/datasets/"+a.Env.DatasetID+"/items", b)
		if err == nil {
			msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			if resp.StatusCode/100 == 2 {
				a.written += len(items)
				return nil
			}
			// 400 schema-validation-error lists the offending fields; keep it in the log
			err = fmt.Errorf("push: HTTP %d: %s", resp.StatusCode, msg)
			if resp.StatusCode < 500 && resp.StatusCode != 429 {
				return err
			}
		}
		if attempt == 4 {
			return err
		}
		time.Sleep(time.Duration(1<<attempt) * time.Second)
	}
}

// Written is the number of items flushed so far.
func (a *Actor) Written() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.written
}

// SetRecord stores a value in the default key-value store and returns the
// API URL of the record (needs the Apify token unless the store is public).
func (a *Actor) SetRecord(ctx context.Context, key, contentType string, body []byte) (string, error) {
	if !a.Env.AtHome {
		dir := filepath.Join(a.Env.Storage, "key_value_stores", a.Env.KVStoreID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		p := filepath.Join(dir, key)
		return "file://" + p, os.WriteFile(p, body, 0o644)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, a.Env.APIBase+"/v2/key-value-stores/"+a.Env.KVStoreID+"/records/"+key, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.Env.Token)
	req.Header.Set("Content-Type", contentType)
	resp, err := a.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("kv put: HTTP %d: %s", resp.StatusCode, b)
	}
	return a.Env.APIBase + "/v2/key-value-stores/" + a.Env.KVStoreID + "/records/" + key, nil
}

// Buffered is the number of pushed items not yet flushed.
func (a *Actor) Buffered() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.buf)
}

// OpenStore returns the id of a named key-value store, creating it on first
// use. Named stores outlive runs, so they carry state between scheduled runs.
// Locally the name is the directory under storage/key_value_stores.
func (a *Actor) OpenStore(ctx context.Context, name string) (string, error) {
	if !a.Env.AtHome {
		return name, os.MkdirAll(filepath.Join(a.Env.Storage, "key_value_stores", name), 0o755)
	}
	resp, err := a.api(ctx, http.MethodPost, "/key-value-stores?name="+name, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("open store %s: HTTP %d: %s", name, resp.StatusCode, b)
	}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Data.ID, nil
}

// GetRecordFrom reads a record; ok=false when it does not exist.
func (a *Actor) GetRecordFrom(ctx context.Context, store, key string) (body []byte, ok bool, err error) {
	if !a.Env.AtHome {
		b, err := os.ReadFile(filepath.Join(a.Env.Storage, "key_value_stores", store, key))
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return b, err == nil, err
	}
	resp, err := a.api(ctx, http.MethodGet, "/key-value-stores/"+store+"/records/"+key, nil)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("kv get: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	return b, err == nil, err
}

// SetRecordIn writes a record into a given store.
func (a *Actor) SetRecordIn(ctx context.Context, store, key, contentType string, body []byte) error {
	if !a.Env.AtHome {
		dir := filepath.Join(a.Env.Storage, "key_value_stores", store)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, key), body, 0o644)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, a.Env.APIBase+"/v2/key-value-stores/"+store+"/records/"+key, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.Env.Token)
	req.Header.Set("Content-Type", contentType)
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("kv put: HTTP %d", resp.StatusCode)
	}
	return nil
}
