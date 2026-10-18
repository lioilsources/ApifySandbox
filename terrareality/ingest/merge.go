package ingest

import "time"

// MissedRunsTTL: a listing absent from its source this many daily runs in a row is deactivated.
const MissedRunsTTL = 2

// Merge applies one daily run of fresh listings onto the stored state, keyed by ID.
// Listings are never deleted; price changes append to PriceHistory. sourcesRun
// limits TTL accounting to sources that actually ran today, so a failed PL run
// does not deactivate Otodom.
func Merge(state map[string]*Listing, fresh []*Listing, sourcesRun map[string]bool, now time.Time) (added, updated, deactivated int) {
	seen := make(map[string]bool, len(fresh))
	for _, f := range fresh {
		seen[f.ID] = true
		old, ok := state[f.ID]
		if !ok {
			state[f.ID] = f
			added++
			continue
		}
		hist := old.PriceHistory
		if old.PriceAmount != f.PriceAmount {
			hist = append(hist, PricePt{At: now, Amount: f.PriceAmount})
		}
		f.FetchedAt = old.FetchedAt
		f.PriceHistory = hist
		state[f.ID] = f
		updated++
	}
	for id, l := range state {
		if seen[id] || !sourcesRun[l.Source] || !l.Active {
			continue
		}
		l.MissedRuns++
		if l.MissedRuns >= MissedRunsTTL {
			l.Active = false
			t := now
			l.InactiveSince = &t
			deactivated++
		}
	}
	return
}
