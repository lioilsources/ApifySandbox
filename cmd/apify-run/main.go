// apify-run starts an Actor through the REST API, waits for it and writes the
// dataset as JSON lines. It prints the run's real cost so it can be compared
// with the estimate made before the run (FINDINGS).
//
//	APIFY_TOKEN=... apify-run -actor compass/crawler-google-places -input in.json -max-items 50 -out leads/apify/raw.jsonl
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/lioilsources/apifysandbox/pkg/apifyrest"
)

func main() {
	actor := flag.String("actor", "", "Actor ID, user/name")
	inputPath := flag.String("input", "", "input JSON file")
	maxItems := flag.Int("max-items", 0, "cap for pay-per-result Actors (0 = none)")
	out := flag.String("out", "-", "output JSONL file, - = stdout")
	dataset := flag.String("dataset", "", "skip the run, only download this dataset")
	estimate := flag.Float64("estimate-usd", 0, "pre-run cost estimate, printed next to the real cost")
	flag.Parse()

	token := os.Getenv("APIFY_TOKEN")
	if token == "" {
		log.Fatal("APIFY_TOKEN is not set (load it from .env, never pass it as a flag)")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	c := apifyrest.New(token)

	dsID := *dataset
	if dsID == "" {
		if *actor == "" || *inputPath == "" {
			log.Fatal("-actor and -input are required")
		}
		raw, err := os.ReadFile(*inputPath)
		if err != nil {
			log.Fatal(err)
		}
		var input json.RawMessage = raw
		t0 := time.Now()
		run, err := c.StartRun(ctx, *actor, input, *maxItems)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("run %s started (%s after request)", run.ID, time.Since(t0).Round(time.Millisecond))
		run, err = c.WaitRun(ctx, run.ID)
		if err != nil {
			log.Fatal(err)
		}
		dur := time.Duration(run.Stats.RunTimeSecs * float64(time.Second))
		log.Printf("run %s %s: runtime %s, wall %s, %.4f CU, $%.4f (estimate $%.4f)",
			run.ID, run.Status, dur.Round(time.Second), time.Since(t0).Round(time.Second),
			run.Stats.ComputeUnits, run.UsageTotalUSD, *estimate)
		if run.Status != "SUCCEEDED" {
			os.Exit(1)
		}
		dsID = run.DefaultDatasetID
	}

	w := os.Stdout
	if *out != "-" {
		f, err := os.Create(*out)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		w = f
	}
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	n, err := c.DatasetItems(ctx, dsID, 1000, func(page []json.RawMessage) error {
		// The API pretty-prints items; compact them so the output is valid JSONL.
		var buf bytes.Buffer
		for _, it := range page {
			buf.Reset()
			if err := json.Compact(&buf, it); err != nil {
				return err
			}
			buf.WriteByte('\n')
			bw.Write(buf.Bytes())
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "dataset %s: %d items\n", dsID, n)
}
