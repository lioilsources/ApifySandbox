// terra-ingest normalizes one daily run of the V4 rental Actors and merges it
// into a JSON state file (append-only price history, TTL deactivation).
//
// Offline (no token, no cost): pass raw Actor output as JSONL files.
//
//	terra-ingest -state data/terra/state.json -raw pringlesbitch/cz-reality-aggregator=raw/cz.jsonl
//
// Live: -dataset actorID=datasetID reads an existing run's dataset over REST;
// -task actorID=taskID reads the task's last successful run (the daily schedule)
// and skips it when that run was already ingested (run ID kept next to the state).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lioilsources/apifysandbox/pkg/apifyrest"
	"github.com/lioilsources/apifysandbox/terrareality/ingest"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(s string) error { *m = append(*m, s); return nil }

func main() {
	statePath := flag.String("state", "data/terra/state.json", "state file")
	var raws, datasets multi
	flag.Var(&raws, "raw", "actorID=file.jsonl (repeatable)")
	flag.Var(&datasets, "dataset", "actorID=datasetID (repeatable, needs APIFY_TOKEN)")
	var tasks multi
	flag.Var(&tasks, "task", "actorID=taskID: last successful run of a saved task (repeatable, needs APIFY_TOKEN)")
	noFX := flag.Bool("no-fx", false, "skip ECB fetch (offline)")
	incremental := flag.Bool("incremental", false, "inputs hold only new listings (onlyNew runs): never deactivate by absence")
	flag.Parse()

	ctx := context.Background()
	now := time.Now().UTC()
	var fx *ingest.Rates
	if !*noFX {
		var err error
		if fx, err = ingest.FetchECB(ctx, &http.Client{Timeout: 20 * time.Second}); err != nil {
			log.Fatalf("fx: %v (use -no-fx offline)", err)
		}
	}

	state := map[string]*ingest.Listing{}
	if b, err := os.ReadFile(*statePath); err == nil {
		if err := json.Unmarshal(b, &state); err != nil {
			log.Fatal(err)
		}
	}

	var fresh []*ingest.Listing
	sourcesRun := map[string]bool{} // every source seen this run
	ttlSources := map[string]bool{} // sources whose run was a full snapshot, so absence means delisted
	skipped := map[string]int{}
	handle := func(actorID string, raw json.RawMessage, delta bool) {
		m, ok := ingest.Mappings[actorID]
		if !ok {
			log.Fatalf("no mapping for %s", actorID)
		}
		l, err := ingest.Normalize(m, raw, fx, now)
		if errors.Is(err, ingest.ErrSkip) {
			skipped[actorID]++
			return
		}
		if err != nil {
			log.Fatal(err)
		}
		sourcesRun[l.Source] = true
		if !delta {
			ttlSources[l.Source] = true
		}
		fresh = append(fresh, l)
	}

	for _, spec := range raws {
		actorID, path, _ := strings.Cut(spec, "=")
		f, err := os.Open(path)
		if err != nil {
			log.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			if len(strings.TrimSpace(sc.Text())) > 0 {
				handle(actorID, append(json.RawMessage(nil), sc.Bytes()...), *incremental)
			}
		}
		f.Close()
		if err := sc.Err(); err != nil {
			log.Fatal(err)
		}
	}
	c := apifyrest.New(os.Getenv("APIFY_TOKEN"))
	seenRunsPath := *statePath + ".runs"
	seenRuns := map[string]bool{}
	if b, err := os.ReadFile(seenRunsPath); err == nil {
		for _, id := range strings.Fields(string(b)) {
			seenRuns[id] = true
		}
	}
	var newRuns, taskDatasets []string
	for _, spec := range tasks {
		actorID, taskID, _ := strings.Cut(spec, "=")
		run, found, err := c.LastTaskRun(ctx, taskID)
		if err != nil {
			log.Fatal(err)
		}
		if !found || seenRuns[run.ID] {
			log.Printf("task %s: no new successful run", taskID)
			continue
		}
		log.Printf("task %s: run %s from %s, $%.4f", taskID, run.ID, run.StartedAt.Format(time.DateTime), run.UsageTotalUSD)
		taskDatasets = append(taskDatasets, actorID+"="+run.DefaultDatasetID)
		newRuns = append(newRuns, run.ID)
	}
	// Saved tasks run with onlyNew, so their datasets are deltas.
	for i, list := range [][]string{datasets, taskDatasets} {
		delta := *incremental || i == 1
		for _, spec := range list {
			actorID, dsID, _ := strings.Cut(spec, "=")
			if _, err := c.DatasetItems(ctx, dsID, 1000, func(page []json.RawMessage) error {
				for _, it := range page {
					handle(actorID, it, delta)
				}
				return nil
			}); err != nil {
				log.Fatal(err)
			}
		}
	}

	added, updated, deact := ingest.Merge(state, fresh, ttlSources, now)
	if err := os.MkdirAll(filepath.Dir(*statePath), 0o755); err != nil {
		log.Fatal(err)
	}
	b, _ := json.MarshalIndent(state, "", " ")
	tmp := *statePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		log.Fatal(err)
	}
	if err := os.Rename(tmp, *statePath); err != nil {
		log.Fatal(err)
	}
	if len(newRuns) > 0 {
		f, err := os.OpenFile(seenRunsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Fprintln(f, strings.Join(newRuns, "\n"))
		f.Close()
	}

	srcs := make([]string, 0, len(sourcesRun))
	for s := range sourcesRun {
		srcs = append(srcs, s)
	}
	sort.Strings(srcs)
	fmt.Printf("%s: +%d new, %d updated, %d deactivated, skipped %v, sources %v, total %d\n",
		now.Format(time.DateOnly), added, updated, deact, skipped, srcs, len(state))
}
