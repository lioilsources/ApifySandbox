// evalcorpus classifies a directory of harvested board files and prints the
// type distribution plus samples per type (development aid, not shipped).
//
//	go run ./internal/evalcorpus <dir-with-board-json-files>
package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lioilsources/czech-building-notices/internal/classify"
	"github.com/lioilsources/czech-building-notices/internal/desky"
)

func main() {
	files, _ := filepath.Glob(filepath.Join(os.Args[1], "*.json"))
	byType := map[classify.Type][]string{}
	total, bad := 0, 0
	for _, f := range files {
		b, _ := os.ReadFile(f)
		items, _, err := desky.Parse(b)
		if err != nil {
			bad++
			continue
		}
		for _, it := range items {
			total++
			typ := classify.Classify(it.Title, it.Description, strings.Join(append(it.Agenda, it.Department), " "))
			t := strings.Join(strings.Fields(it.Title), " ")
			byType[typ] = append(byType[typ], t[:min(len(t), 150)])
		}
	}
	fmt.Printf("files %d (unparsable %d), items %d\n", len(files), bad, total)
	var keys []string
	for k := range byType {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	r := rand.New(rand.NewSource(99))
	n := 6
	if len(os.Args) > 2 {
		fmt.Sscan(os.Args[2], &n)
	}
	for _, k := range keys {
		v := byType[classify.Type(k)]
		fmt.Printf("== %-16q %6d\n", k, len(v))
		if k == "" {
			if len(os.Args) > 3 && os.Args[3] == "miss" {
				var miss []string
				for _, s := range v {
					f := strings.ToLower(s)
					if strings.Contains(f, "stavb") || strings.Contains(f, "povolen") || strings.Contains(f, "zahájen") || strings.Contains(f, "záměr") {
						miss = append(miss, s)
					}
				}
				r.Shuffle(len(miss), func(i, j int) { miss[i], miss[j] = miss[j], miss[i] })
				fmt.Printf("   (%d unclassified with building words)\n", len(miss))
				for _, s := range miss[:min(40, len(miss))] {
					fmt.Println("   -", s)
				}
			}
			continue
		}
		r.Shuffle(len(v), func(i, j int) { v[i], v[j] = v[j], v[i] })
		for _, s := range v[:min(n, len(v))] {
			fmt.Println("   ", s)
		}
	}
}
