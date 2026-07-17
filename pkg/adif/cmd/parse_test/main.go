package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/FtlC-ian/radioledger/pkg/adif"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <adif-file>\n", os.Args[0])
		os.Exit(1)
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	header, records, err := adif.ParseAll(context.Background(), f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Parse error: %v\n", err)
		os.Exit(1)
	}

	// Header info
	if header != nil {
		fmt.Printf("ADIF Version: %s\n", header.Get("ADIF_VER"))
		fmt.Printf("Program: %s %s\n", header.Get("PROGRAMID"), header.Get("PROGRAMVERSION"))
	}

	fmt.Printf("Total QSOs: %d\n\n", len(records))

	// Collect stats
	modes := map[string]int{}
	bands := map[string]int{}
	countries := map[string]int{}
	grids := map[string]int{}
	callsigns := map[string]bool{}
	var errors []string

	for _, rec := range records {
		call := rec.Get("CALL")
		mode := rec.Get("MODE")
		band := rec.Get("BAND")
		country := rec.Get("COUNTRY")
		grid := rec.Get("GRIDSQUARE")

		if call != "" {
			callsigns[strings.ToUpper(call)] = true
		}
		if mode != "" {
			modes[mode]++
		}
		if band != "" {
			bands[band]++
		}
		if country != "" {
			countries[country]++
		}
		if grid != "" && len(grid) >= 4 {
			grids[grid[:4]]++
		}

		// Try callsign parsing
		if call != "" {
			parsed := adif.ParseCallsign(call)
			if parsed.Base == "" {
				errors = append(errors, fmt.Sprintf("Could not parse callsign: %s", call))
			}
		}

		// Validate date
		date := rec.Get("QSO_DATE")
		if date != "" {
			if err := adif.ValidateDate(date); err != nil {
				errors = append(errors, fmt.Sprintf("Invalid date %q for %s: %v", date, call, err))
			}
		}

		// Validate time
		timeOn := rec.Get("TIME_ON")
		if timeOn != "" {
			if err := adif.ValidateTime(timeOn); err != nil {
				errors = append(errors, fmt.Sprintf("Invalid time %q for %s: %v", timeOn, call, err))
			}
		}
	}

	// Print stats
	fmt.Printf("Unique callsigns: %d\n", len(callsigns))
	fmt.Printf("Unique grid squares: %d\n", len(grids))
	fmt.Printf("Unique countries: %d\n\n", len(countries))

	fmt.Println("=== Modes ===")
	printTopN(modes, 15)

	fmt.Println("\n=== Bands ===")
	printTopN(bands, 15)

	fmt.Println("\n=== Top 20 Countries ===")
	printTopN(countries, 20)

	// Callsign parsing examples
	fmt.Println("\n=== Callsign Parsing Samples ===")
	samples := []string{}
	for _, rec := range records {
		call := rec.Get("CALL")
		if call != "" && (strings.Contains(call, "/") || len(samples) < 5) {
			found := false
			for _, s := range samples {
				if s == call {
					found = true
					break
				}
			}
			if !found {
				samples = append(samples, call)
				if len(samples) >= 20 {
					break
				}
			}
		}
	}
	for _, call := range samples {
		p := adif.ParseCallsign(call)
		fmt.Printf("  %s → base=%s prefix=%s suffix=%s wpx=%s\n",
			call, p.Base, p.PrefixOverride, p.Suffix, p.WPXPrefix)
	}

	// Errors
	if len(errors) > 0 {
		fmt.Printf("\n=== Validation Issues (%d) ===\n", len(errors))
		for i, e := range errors {
			if i >= 20 {
				fmt.Printf("  ... and %d more\n", len(errors)-20)
				break
			}
			fmt.Printf("  %s\n", e)
		}
	} else {
		fmt.Println("\n✓ No validation issues found!")
	}
}

func printTopN(m map[string]int, n int) {
	type kv struct {
		Key   string
		Value int
	}
	var sorted []kv
	for k, v := range m {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})
	for i, item := range sorted {
		if i >= n {
			break
		}
		fmt.Printf("  %4d  %s\n", item.Value, item.Key)
	}
}
