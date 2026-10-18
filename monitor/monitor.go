// Package monitor turns "Actor run succeeded" webhooks from scheduled Reddit /
// app-review scrapers into notifications about items not seen before.
package monitor

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"strings"
	"sync"
)

// Webhook is Apify's default payload template.
type Webhook struct {
	EventType string `json:"eventType"` // ACTOR.RUN.SUCCEEDED, ...
	EventData struct {
		ActorID    string `json:"actorId"`
		ActorRunID string `json:"actorRunId"`
	} `json:"eventData"`
	Resource struct {
		ID               string `json:"id"`
		ActID            string `json:"actId"`
		Status           string `json:"status"`
		DefaultDatasetID string `json:"defaultDatasetId"`
	} `json:"resource"`
}

// Seen is an append-only set of IDs persisted as one ID per line.
type Seen struct {
	mu  sync.Mutex
	set map[string]bool
	f   *os.File
}

func OpenSeen(path string) (*Seen, error) {
	s := &Seen{set: map[string]bool{}}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			s.set[sc.Text()] = true
		}
		f.Close()
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	s.f = f
	return s, nil
}

// Add returns true when id is new and records it.
func (s *Seen) Add(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.set[id] {
		return false, nil
	}
	if _, err := fmt.Fprintln(s.f, id); err != nil {
		return false, err
	}
	s.set[id] = true
	return true, nil
}

func (s *Seen) Close() error { return s.f.Close() }

// Item is the minimum shown for one Reddit post or store review.
type Item struct {
	Key    string `json:"key"` // actor-scoped stable ID used for dedup
	Actor  string `json:"actor"`
	Kind   string `json:"kind"` // reddit | review | other
	Title  string `json:"title"`
	Text   string `json:"text"`
	URL    string `json:"url"`
	Where  string `json:"where"` // subreddit or app
	Rating string `json:"rating,omitempty"`
}

func first(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
				return s
			}
		}
	}
	return ""
}

// ParseItem maps Reddit and App Store / Google Play scraper output. Field names
// differ between Store Actors, so candidates are tried in order.
func ParseItem(actorID string, raw json.RawMessage) Item {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	it := Item{
		Actor:  actorID,
		Title:  first(m, "title", "reviewTitle"),
		Text:   first(m, "body", "text", "content", "reviewText", "review"),
		URL:    first(m, "url", "link", "permalink", "reviewUrl"),
		Where:  first(m, "communityName", "subreddit", "parsedCommunityName", "appTitle", "appName", "appId"),
		Rating: first(m, "score", "rating", "stars", "upVotes"),
	}
	id := first(m, "id", "reviewId", "parsedId", "postId")
	if id == "" {
		h := sha1.Sum(raw)
		id = hex.EncodeToString(h[:8])
	}
	it.Key = actorID + ":" + id
	switch {
	case m["communityName"] != nil || m["subreddit"] != nil || strings.Contains(it.URL, "reddit.com"):
		it.Kind = "reddit"
	case m["reviewId"] != nil || m["appId"] != nil || m["stars"] != nil:
		it.Kind = "review"
	default:
		it.Kind = "other"
	}
	return it
}

// Message renders a Telegram HTML message (notify.sh expects HTML).
func Message(items []Item, max int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>Apify monitor: %d nových</b>\n", len(items))
	for i, it := range items {
		if i == max {
			fmt.Fprintf(&b, "… a %d dalších", len(items)-max)
			break
		}
		title := it.Title
		if title == "" {
			title = it.Text
		}
		if r := []rune(title); len(r) > 120 {
			title = string(r[:120]) + "…"
		}
		meta := it.Where
		if it.Rating != "" {
			meta += " · " + it.Rating
		}
		fmt.Fprintf(&b, "\n• [%s] <a href=\"%s\">%s</a> <i>%s</i>", it.Kind, html.EscapeString(it.URL), html.EscapeString(title), html.EscapeString(meta))
	}
	return b.String()
}
