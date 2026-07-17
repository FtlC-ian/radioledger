package callsign

import (
	"bytes"
	"context"
	"testing"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

func TestParseJJ1WTLCSVData_UTF8(t *testing.T) {
	csvText := "Call,JCC#/JCG#,Prefecture,City/Gun,AJA#/Hamlog#,Ward/Town/Village,Licensed/Renewed Date (5-year valid),License class and Fixed/Mobile,#th Station under the same Call (typically Fixed(>50W) and Mobile(<=50W)),JARL Membersip (Buro available unless otherwise indicated in the next column),Buro unavailability,Licensee (disclosed only in case of a club station),\n" +
		"JA1BF,1101,Kanagawa,Yokohama,110106,Hodogaya,2022-01-14,1AM,1,JARL,,,\n" +
		"8J1YAA,1001,Tokyo,Chiyoda,,,2023-05-01,1AF,1,,,Tokyo Amateur Radio Club,\n"

	result, err := ParseJJ1WTLCSVData(context.Background(), []byte(csvText))
	if err != nil {
		t.Fatalf("ParseJJ1WTLCSVData: %v", err)
	}

	if result.Processed != 2 {
		t.Fatalf("processed: got %d, want 2", result.Processed)
	}
	if result.Skipped != 0 {
		t.Fatalf("skipped: got %d, want 0", result.Skipped)
	}
	if len(result.Records) != 2 {
		t.Fatalf("records: got %d, want 2", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Callsign != "JA1BF" {
		t.Errorf("callsign: got %q, want JA1BF", rec.Callsign)
	}
	if rec.Source != "jj1wtl" {
		t.Errorf("source: got %q, want jj1wtl", rec.Source)
	}
	if rec.StateProvince != "Kanagawa" {
		t.Errorf("state_province: got %q, want Kanagawa", rec.StateProvince)
	}
	if rec.City != "Yokohama Hodogaya" {
		t.Errorf("city: got %q, want %q", rec.City, "Yokohama Hodogaya")
	}
	if rec.Country != "Japan" {
		t.Errorf("country: got %q, want Japan", rec.Country)
	}
	if rec.Status != "active" {
		t.Errorf("status: got %q, want active", rec.Status)
	}
	if rec.GrantDate == nil {
		t.Errorf("grant_date: got nil, expected parsed date")
	}

	rec2 := result.Records[1]
	if rec2.Callsign != "8J1YAA" {
		t.Errorf("callsign[1]: got %q, want 8J1YAA", rec2.Callsign)
	}
	if rec2.FullName != "Tokyo Amateur Radio Club" {
		t.Errorf("full_name[1]: got %q", rec2.FullName)
	}
}

func TestParseJJ1WTLCSVData_ShiftJISFallback(t *testing.T) {
	utf8CSV := "コールサイン,都道府県,市区町村,名前\n" +
		"JA1ZZZ,東京都,新宿区,東京クラブ局\n"

	var sjis bytes.Buffer
	w := transform.NewWriter(&sjis, japanese.ShiftJIS.NewEncoder())
	if _, err := w.Write([]byte(utf8CSV)); err != nil {
		t.Fatalf("encode shift-jis: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close encoder: %v", err)
	}

	result, err := ParseJJ1WTLCSVData(context.Background(), sjis.Bytes())
	if err != nil {
		t.Fatalf("ParseJJ1WTLCSVData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Callsign != "JA1ZZZ" {
		t.Errorf("callsign: got %q, want JA1ZZZ", rec.Callsign)
	}
	if rec.StateProvince != "東京都" {
		t.Errorf("state_province: got %q, want 東京都", rec.StateProvince)
	}
	if rec.City != "新宿区" {
		t.Errorf("city: got %q, want 新宿区", rec.City)
	}
	if rec.FullName != "東京クラブ局" {
		t.Errorf("full_name: got %q, want 東京クラブ局", rec.FullName)
	}
}

func TestDiscoverJJ1WTLLatestCSVURLFromHTML(t *testing.T) {
	html := `
		<a href="http://motobayashi.net/callbook/ever/20230823/offline-callbook-ja-20230823-en.csv">2023</a>
		<a href="http://motobayashi.net/callbook/ever/20250913/offline-callbook-ja-20250913-en.csv">2025</a>
		<a href="http://motobayashi.net/callbook/ever/20240828/offline-callbook-ja-20240828-en.csv">2024</a>
	`

	got, err := discoverJJ1WTLLatestCSVURLFromHTML(html)
	if err != nil {
		t.Fatalf("discover latest url: %v", err)
	}

	want := "http://motobayashi.net/callbook/ever/20250913/offline-callbook-ja-20250913-en.csv"
	if got != want {
		t.Fatalf("latest url: got %q, want %q", got, want)
	}
}
