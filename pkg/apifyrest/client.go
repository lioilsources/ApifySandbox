// Package apifyrest is a minimal client for the Apify REST API v2.
//
// Apify ships official clients only for JS and Python, so Go talks to
// https://api.apify.com/v2 directly. Only what the sandbox needs is covered:
// start/wait/run-sync of Actors, paginated dataset reads, dataset pushes,
// key-value records and Standby HTTP calls.
package apifyrest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.apify.com/v2"

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func New(token string) *Client {
	return &Client{BaseURL: DefaultBaseURL, Token: token, HTTP: &http.Client{Timeout: 6 * time.Minute}}
}

// StatusError is a non-2xx answer from the API.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string { return fmt.Sprintf("apify: HTTP %d: %s", e.Code, e.Body) }

// Run is the subset of the run object the sandbox reads.
type Run struct {
	ID                     string     `json:"id"`
	ActID                  string     `json:"actId"`
	Status                 string     `json:"status"` // READY|RUNNING|SUCCEEDED|FAILED|TIMING-OUT|TIMED-OUT|ABORTING|ABORTED
	StartedAt              time.Time  `json:"startedAt"`
	FinishedAt             *time.Time `json:"finishedAt"`
	DefaultDatasetID       string     `json:"defaultDatasetId"`
	DefaultKeyValueStoreID string     `json:"defaultKeyValueStoreId"`
	UsageTotalUSD          float64    `json:"usageTotalUsd"`
	Stats                  struct {
		ComputeUnits float64 `json:"computeUnits"`
		RunTimeSecs  float64 `json:"runTimeSecs"`
	} `json:"stats"`
}

func (r *Run) Terminal() bool {
	switch r.Status {
	case "SUCCEEDED", "FAILED", "TIMED-OUT", "ABORTED":
		return true
	}
	return false
}

// ActorPath turns "user/actor" into the "user~actor" form the API expects.
func ActorPath(id string) string { return url.PathEscape(strings.Replace(id, "/", "~", 1)) }

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, out any) (http.Header, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token) // header, never query: tokens in URLs end up in logs
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return resp.Header, &StatusError{Code: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return resp.Header, err
	}
	return resp.Header, json.NewDecoder(resp.Body).Decode(out)
}

type envelope[T any] struct {
	Data T `json:"data"`
}

// StartRun starts an Actor asynchronously. maxItems caps pay-per-result Actors (0 = no cap).
func (c *Client) StartRun(ctx context.Context, actorID string, input any, maxItems int) (*Run, error) {
	q := url.Values{}
	if maxItems > 0 {
		q.Set("maxItems", strconv.Itoa(maxItems))
	}
	var env envelope[Run]
	_, err := c.do(ctx, http.MethodPost, "/acts/"+ActorPath(actorID)+"/runs", q, input, &env)
	return &env.Data, err
}

// GetRun returns the run, waiting server-side up to wait (API max 60 s).
func (c *Client) GetRun(ctx context.Context, runID string, wait time.Duration) (*Run, error) {
	q := url.Values{}
	if wait > 0 {
		q.Set("waitForFinish", strconv.Itoa(int(wait.Seconds())))
	}
	var env envelope[Run]
	_, err := c.do(ctx, http.MethodGet, "/actor-runs/"+url.PathEscape(runID), q, nil, &env)
	return &env.Data, err
}

// WaitRun long-polls until the run is terminal or ctx ends.
func (c *Client) WaitRun(ctx context.Context, runID string) (*Run, error) {
	for {
		r, err := c.GetRun(ctx, runID, 60*time.Second)
		if err != nil {
			return nil, err
		}
		if r.Terminal() {
			return r, nil
		}
		if err := ctx.Err(); err != nil {
			return r, err
		}
	}
}

// RunSyncItems runs an Actor and returns its dataset items in one call.
// The API cuts the connection after 300 s; use StartRun+WaitRun for longer runs.
func (c *Client) RunSyncItems(ctx context.Context, actorID string, input any, maxItems int) ([]json.RawMessage, error) {
	q := url.Values{"format": {"json"}, "clean": {"true"}}
	if maxItems > 0 {
		q.Set("maxItems", strconv.Itoa(maxItems))
	}
	var items []json.RawMessage
	_, err := c.do(ctx, http.MethodPost, "/acts/"+ActorPath(actorID)+"/run-sync-get-dataset-items", q, input, &items)
	return items, err
}

// DatasetItems pages through a dataset and calls fn for each page.
func (c *Client) DatasetItems(ctx context.Context, datasetID string, pageSize int, fn func([]json.RawMessage) error) (int, error) {
	if pageSize <= 0 {
		pageSize = 1000
	}
	total := 0
	for offset := 0; ; offset += pageSize {
		q := url.Values{"format": {"json"}, "clean": {"true"}, "offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(pageSize)}}
		var page []json.RawMessage
		if _, err := c.do(ctx, http.MethodGet, "/datasets/"+url.PathEscape(datasetID)+"/items", q, nil, &page); err != nil {
			return total, err
		}
		if len(page) == 0 {
			return total, nil
		}
		total += len(page)
		if err := fn(page); err != nil {
			return total, err
		}
		if len(page) < pageSize {
			return total, nil
		}
	}
}

// PushItems appends items to a dataset (an Actor writes its output this way).
func (c *Client) PushItems(ctx context.Context, datasetID string, items any) error {
	_, err := c.do(ctx, http.MethodPost, "/datasets/"+url.PathEscape(datasetID)+"/items", nil, items, nil)
	return err
}

// GetRecord reads a key-value store record into out; found is false on 404.
func (c *Client) GetRecord(ctx context.Context, storeID, key string, out any) (found bool, err error) {
	_, err = c.do(ctx, http.MethodGet, "/key-value-stores/"+url.PathEscape(storeID)+"/records/"+url.PathEscape(key), nil, nil, out)
	if se, ok := err.(*StatusError); ok && se.Code == 404 {
		return false, nil
	}
	return err == nil, err
}

// PutRecord stores v as JSON under key.
func (c *Client) PutRecord(ctx context.Context, storeID, key string, v any) error {
	_, err := c.do(ctx, http.MethodPut, "/key-value-stores/"+url.PathEscape(storeID)+"/records/"+url.PathEscape(key), nil, v, nil)
	return err
}

// StandbyURL is the Standby base URL of an Actor, e.g. https://user--actor.apify.actor.
func StandbyURL(actorID string) string {
	return "https://" + strings.Replace(actorID, "/", "--", 1) + ".apify.actor"
}

// Standby sends a GET to a Standby Actor's web server and decodes JSON into out.
func (c *Client) Standby(ctx context.Context, actorID, path string, q url.Values, out any) error {
	sc := *c
	sc.BaseURL = StandbyURL(actorID)
	_, err := sc.do(ctx, http.MethodGet, path, q, nil, out)
	return err
}

// RunTask starts a saved Actor task (its stored input and options apply).
func (c *Client) RunTask(ctx context.Context, taskID string) (*Run, error) {
	var env envelope[Run]
	_, err := c.do(ctx, http.MethodPost, "/actor-tasks/"+ActorPath(taskID)+"/runs", nil, nil, &env)
	return &env.Data, err
}

// LastTaskRun returns the task's last successful run; found is false when it has none.
func (c *Client) LastTaskRun(ctx context.Context, taskID string) (*Run, bool, error) {
	var env envelope[Run]
	_, err := c.do(ctx, http.MethodGet, "/actor-tasks/"+ActorPath(taskID)+"/runs/last", url.Values{"status": {"SUCCEEDED"}}, nil, &env)
	if se, ok := err.(*StatusError); ok && se.Code == 404 {
		return nil, false, nil
	}
	return &env.Data, err == nil, err
}
