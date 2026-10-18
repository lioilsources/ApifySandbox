package monitor

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseItemAndDedup(t *testing.T) {
	reddit := ParseItem("trudax/reddit-scraper-lite", json.RawMessage(`{"id":"t3_abc","title":"Best flutter game <3","url":"https://www.reddit.com/r/FlutterDev/abc","communityName":"r/FlutterDev","upVotes":12}`))
	review := ParseItem("neatrat/google-play-store-reviews-scraper", json.RawMessage(`{"reviewId":"gp1","text":"Crashes on start","stars":1,"appId":"now.olin.game"}`))
	if reddit.Kind != "reddit" || reddit.Key != "trudax/reddit-scraper-lite:t3_abc" || review.Kind != "review" || review.Key != "neatrat/google-play-store-reviews-scraper:gp1" {
		t.Fatalf("%+v %+v", reddit, review)
	}
	msg := Message([]Item{reddit, review}, 10)
	if !strings.Contains(msg, "&lt;3") || !strings.Contains(msg, "Crashes on start") {
		t.Fatal(msg)
	}

	path := filepath.Join(t.TempDir(), "seen.txt")
	s, err := OpenSeen(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Add("a"); !ok {
		t.Fatal("first add")
	}
	if ok, _ := s.Add("a"); ok {
		t.Fatal("duplicate add")
	}
	s.Close()
	s, _ = OpenSeen(path)
	if ok, _ := s.Add("a"); ok {
		t.Fatal("not persisted")
	}
}
