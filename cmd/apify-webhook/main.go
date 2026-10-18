// apify-webhook receives ACTOR.RUN.SUCCEEDED webhooks from scheduled monitor
// Actors, pulls the run's dataset, keeps only items not seen before, appends
// them to a JSONL queue (for the LLM triage batch in the SPARK window) and
// sends a short notification.
//
// Target host is JODA (always on); SPARK may power off under load. The
// webhook's headersTemplate must send X-Apify-Webhook-Secret.
//
//	APIFY_TOKEN=... WEBHOOK_SECRET=... apify-webhook -addr 127.0.0.1:8098 -data /data \
//	    -notify-cmd 'ssh spark ~/deploy/WorldLibraryProject/deploy/spark/notify.sh'
package main

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lioilsources/apifysandbox/monitor"
	"github.com/lioilsources/apifysandbox/pkg/apifyrest"
)

type server struct {
	api       *apifyrest.Client
	secret    string
	seenItems *monitor.Seen
	seenRuns  *monitor.Seen
	queuePath string
	notifyCmd string
	qmu       sync.Mutex
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8098", "listen address (cloudflared points here)")
	dataDir := flag.String("data", "data/monitor", "state directory")
	notifyCmd := flag.String("notify-cmd", "", "command that gets the HTML message as last arg; empty = log only")
	flag.Parse()

	token, secret := os.Getenv("APIFY_TOKEN"), os.Getenv("WEBHOOK_SECRET")
	if token == "" || secret == "" {
		log.Fatal("APIFY_TOKEN and WEBHOOK_SECRET must be set (.env, chmod 600)")
	}
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	items, err := monitor.OpenSeen(filepath.Join(*dataDir, "seen_items.txt"))
	if err != nil {
		log.Fatal(err)
	}
	runs, err := monitor.OpenSeen(filepath.Join(*dataDir, "seen_runs.txt"))
	if err != nil {
		log.Fatal(err)
	}
	s := &server{api: apifyrest.New(token), secret: secret, seenItems: items, seenRuns: runs,
		queuePath: filepath.Join(*dataDir, "queue.jsonl"), notifyCmd: *notifyCmd}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /apify/webhook", s.webhook)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") })
	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func (s *server) webhook(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Apify-Webhook-Secret")), []byte(s.secret)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var wh monitor.Webhook
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&wh); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	if wh.EventType != "ACTOR.RUN.SUCCEEDED" || wh.Resource.DefaultDatasetID == "" {
		log.Printf("ignored %s run=%s status=%s", wh.EventType, wh.EventData.ActorRunID, wh.Resource.Status)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Apify retries until it gets 2xx within 30 s, so answer now and work in the background.
	// Duplicate deliveries of the same run are dropped by seenRuns.
	if fresh, _ := s.seenRuns.Add(wh.EventData.ActorRunID); !fresh {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	go s.process(wh)
}

func (s *server) process(wh monitor.Webhook) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	actorID := wh.EventData.ActorID
	var news []monitor.Item
	n, err := s.api.DatasetItems(ctx, wh.Resource.DefaultDatasetID, 500, func(page []json.RawMessage) error {
		for _, raw := range page {
			it := monitor.ParseItem(actorID, raw)
			fresh, err := s.seenItems.Add(it.Key)
			if err != nil {
				return err
			}
			if fresh {
				news = append(news, it)
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("run %s: dataset: %v", wh.EventData.ActorRunID, err)
		return
	}
	log.Printf("run %s: %d items, %d new", wh.EventData.ActorRunID, n, len(news))
	if len(news) == 0 {
		return
	}
	if err := s.enqueue(news); err != nil {
		log.Printf("queue: %v", err)
	}
	s.notify(monitor.Message(news, 10))
}

func (s *server) enqueue(items []monitor.Item) error {
	s.qmu.Lock()
	defer s.qmu.Unlock()
	f, err := os.OpenFile(s.queuePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	enc := json.NewEncoder(bw)
	for _, it := range items {
		if err := enc.Encode(it); err != nil {
			return err
		}
	}
	return bw.Flush()
}

func (s *server) notify(msg string) {
	if s.notifyCmd == "" {
		log.Printf("notify (dry):\n%s", msg)
		return
	}
	args := strings.Fields(s.notifyCmd)
	cmd := exec.Command(args[0], append(args[1:], msg)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("notify: %v: %s", err, out)
	}
}
