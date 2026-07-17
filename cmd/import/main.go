// Command import loads bank SMS exports into CashPulse.
//
// Supports:
//   - plain txt lines: "001  【邮储银行】..."
//   - iPhone CSV export with a "text" column (only SMS body is used)
//
//	go run ./cmd/import -file tmp/95580_iPhone完整短信.csv -db ./data/cashpulse.db
package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"cashpulse/internal/parser"
	"cashpulse/internal/service"
	"cashpulse/internal/store"
)

func main() {
	file := flag.String("file", "", "path to export .txt or .csv")
	dbPath := flag.String("db", "./data/cashpulse.db", "sqlite path")
	tz := flag.String("tz", "Asia/Shanghai", "timezone for parsing SMS times")
	reset := flag.Bool("reset", false, "delete existing db file first")
	flag.Parse()

	if *file == "" {
		log.Fatal("usage: import -file tmp/xxx.csv|txt [-db ./data/cashpulse.db] [-reset]")
	}
	if *reset {
		_ = os.Remove(*dbPath)
		_ = os.Remove(*dbPath + "-wal")
		_ = os.Remove(*dbPath + "-shm")
	}

	loc, err := time.LoadLocation(*tz)
	if err != nil {
		log.Fatalf("tz: %v", err)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	svc := service.New(st, parser.New(loc), loc)
	ctx := context.Background()

	texts, err := loadTexts(*file)
	if err != nil {
		log.Fatal(err)
	}

	var total, okN, ignored, failed, skipped int
	seen := map[string]struct{}{}
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if _, dup := seen[text]; dup {
			skipped++
			continue
		}
		seen[text] = struct{}{}
		total++
		resp, err := svc.IngestSMS(ctx, service.IngestSMSRequest{
			Text:   text,
			Source: "import-" + filepath.Ext(*file),
		})
		if err != nil {
			failed++
			fmt.Printf("ERR  %s\n     %v\n", truncate(text, 80), err)
			continue
		}
		switch {
		case resp.Ignored:
			ignored++
		case resp.Status == "ok":
			okN++
		default:
			failed++
			fmt.Printf("FAIL %s\n     %s\n", truncate(text, 80), resp.ParseError)
		}
	}

	fmt.Printf("\nimport done: unique=%d ok=%d ignored=%d failed=%d skipped_dup=%d db=%s\n",
		total, okN, ignored, failed, skipped, *dbPath)
}

func loadTexts(path string) ([]string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".csv" {
		return loadCSVTexts(path)
	}
	return loadTXTTexts(path)
}

func loadCSVTexts(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("empty csv")
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
		return nil, fmt.Errorf("csv missing text column: %v", header)
	}
	di, hasDir := idx["direction"]
	var out []string
	for _, row := range rows[1:] {
		if ti >= len(row) {
			continue
		}
		if hasDir && di < len(row) && row[di] != "" && row[di] != "received" {
			continue
		}
		out = append(out, row[ti])
	}
	return out, nil
}

func loadTXTTexts(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	reLine := regexp.MustCompile(`^\d{3}\s+(【.+)$`)
	var out []string
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		m := reLine.FindStringSubmatch(line)
		if m == nil {
			// also accept raw 【邮储 lines
			if strings.HasPrefix(line, "【") {
				out = append(out, line)
			}
			continue
		}
		out = append(out, m[1])
	}
	return out, sc.Err()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
