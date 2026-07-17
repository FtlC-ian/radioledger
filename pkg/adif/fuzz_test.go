package adif_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/FtlC-ian/radioledger/pkg/adif"
)

// FuzzParser runs the ADIF parser against random/mutated byte inputs.
// Any input must never cause a panic, infinite loop, or OOM.
// Run with: go test -fuzz=FuzzParser -fuzztime=30s
func FuzzParser(f *testing.F) {
	// Seed corpus — representative inputs the fuzzer will mutate from
	seeds := []string{
		"<ADIF_VER:5>3.1.4\n<EOH>\n<CALL:4>W1AW <BAND:3>20m <EOR>\n",
		"<CALL:4>W1AW <EOR>\n",
		"",
		"<",
		"<CALL:999",
		"<CALL:0> <EOR>",
		"<EOH>\n<CALL:4>W1AW <EOR>\n<CALL:4>K5XX <EOR>\n",
		"<ADIF_VER:5>3.1.4<EOH><CALL:4>W1AW<EOR>",
		"<adif_ver:5>3.1.4<eoh><call:4>w1aw<eor>",
		"\x00\x01\x02\x03<CALL:4>W1AW<EOR>",
		"\xEF\xBB\xBF<ADIF_VER:5>3.1.4<EOH><CALL:4>W1AW<EOR>",
		"<ADIF_VER:5>3.1.4<EOH><COMMENT:10>hello>world<EOR>",
		"<AVERYLONGFIELDNAMETHATEXCEEDSNORMALADIFSPEC:4>test<EOR>",
		"<EOH><EOR><EOR><EOR>",
		"<EOH>",
	}

	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// The parser must never panic regardless of input.
		ctx := context.Background()
		opts := adif.ParserOptions{
			MaxFieldLen: 1024 * 1024, // 1MB limit keeps fuzz runs fast
			MaxRecords:  1000,
		}

		// Test ParseBytes (convenience wrapper)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ParseBytes panicked on input %q: %v", data, r)
				}
			}()
			_, _, _ = adif.ParseBytes(ctx, data)
		}()

		// Test the streaming API separately
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("streaming parser panicked on input %q: %v", data, r)
				}
			}()

			p := adif.NewParserWithOptions(bytes.NewReader(data), opts)
			_, _ = p.Header(ctx)
			for i := 0; i < 1000; i++ {
				_, err := p.Next(ctx)
				if err == io.EOF || err == adif.ErrTooManyRecords {
					break
				}
				if err != nil {
					break
				}
			}
		}()
	})
}
