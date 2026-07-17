package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"time"

	"cashpulse/internal/parser"
)

func main() {
	path := "tmp/95580_iPhone完整短信.csv"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		panic(err)
	}
	if len(rows) < 2 {
		panic("empty csv")
	}

	header := rows[0]
	idx := map[string]int{}
	bom := string(rune(0xFEFF))
	for i, h := range header {
		h = strings.TrimSpace(h)
		h = strings.TrimPrefix(h, bom)
		idx[h] = i
	}
	ti, ok := idx["text"]
	if !ok {
		panic(fmt.Sprintf("no text column in %v", header))
	}
	di, hasDir := idx["direction"]

	p := parser.New(time.FixedZone("CST", 8*3600))
	seen := map[string]struct{}{}
	var total, okN, ign, fail, noBal int
	kindC := map[string]int{}
	merC := map[string]int{}
	var failSamples, otherSamples []string

	for _, row := range rows[1:] {
		if ti >= len(row) {
			continue
		}
		text := strings.TrimSpace(row[ti])
		if text == "" {
			continue
		}
		if hasDir && di < len(row) && row[di] != "" && row[di] != "received" {
			continue
		}
		if _, dup := seen[text]; dup {
			continue
		}
		seen[text] = struct{}{}
		total++

		res, err := p.Parse(text, time.Now())
		if err != nil {
			fail++
			if len(failSamples) < 25 {
				failSamples = append(failSamples, text)
			}
			continue
		}
		if res.Ignored {
			ign++
			continue
		}
		okN++
		t := res.Transaction
		kindC[string(t.Kind)]++
		mer := t.MerchantNorm
		if mer == "" {
			mer = t.Merchant
		}
		merC[mer]++
		if !t.BalanceKnown {
			noBal++
		}
		if t.Kind == "other" || t.Category == "其他" {
			if len(otherSamples) < 20 {
				otherSamples = append(otherSamples, fmt.Sprintf("%s => mer=%q cat=%q kind=%q norm=%q",
					text, t.Merchant, t.Category, t.Kind, t.MerchantNorm))
			}
		}
	}

	covered := 0.0
	if total > 0 {
		covered = 100 * float64(okN+ign) / float64(total)
	}
	fmt.Printf("unique=%d ok=%d ignored=%d fail=%d covered=%.2f%% no_balance=%d\n",
		total, okN, ign, fail, covered, noBal)
	fmt.Println("kinds:", kindC)

	type kv struct {
		k string
		v int
	}
	list := make([]kv, 0, len(merC))
	for k, v := range merC {
		list = append(list, kv{k, v})
	}
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].v > list[i].v {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	fmt.Println("top merchants:")
	for i := 0; i < len(list) && i < 20; i++ {
		fmt.Printf("  %4d  %s\n", list[i].v, list[i].k)
	}
	fmt.Println("FAIL samples:")
	for _, s := range failSamples {
		fmt.Println(" -", s)
	}
	fmt.Println("OTHER samples:")
	for _, s := range otherSamples {
		fmt.Println(" -", s)
	}
}
