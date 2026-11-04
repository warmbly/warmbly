package csv

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
)

func TestMaxFieldsPerRecord(t *testing.T) {
	for _, reuse := range []bool{false, true} {
		for _, input := range []string{"a,b,c", "a,b,", `"a","b","c"`, "\"a\nb\",c,d", "a,b,\"unfinished"} {
			t.Run(fmt.Sprintf("%q/reuse=%t", input, reuse), func(t *testing.T) {
				r := NewReader(strings.NewReader(input))
				r.MaxFieldsPerRecord = 2
				r.ReuseRecord = reuse
				got, err := r.Read()
				var parseErr *ParseError
				if got != nil || !errors.Is(err, ErrFieldLimit) || !errors.As(err, &parseErr) {
					t.Fatalf("record=%q error=%v", got, err)
				}
				if parseErr.StartLine != 1 || parseErr.Line < 1 || parseErr.Column < 1 {
					t.Fatalf("position=%+v", parseErr)
				}
			})
		}
	}
	for _, input := range []string{"a", "a,b", "a,", `"a,b",c`, "\"a\r\nb\",c", "a,b\nc,d\ne"} {
		r := NewReader(strings.NewReader(input))
		r.MaxFieldsPerRecord = 2
		r.FieldsPerRecord = -1
		if _, err := r.ReadAll(); err != nil {
			t.Fatalf("%q: %v", input, err)
		}
	}
	r := NewReader(strings.NewReader("a,b\nc,d,e"))
	r.MaxFieldsPerRecord = 2
	if got, err := r.ReadAll(); got != nil || !errors.Is(err, ErrFieldLimit) {
		t.Fatalf("ReadAll=%q, %v", got, err)
	}
	r = NewReader(strings.NewReader("a,b\n\"c\nd\",e,f"))
	r.MaxFieldsPerRecord, r.ReuseRecord = 2, true
	if _, err := r.Read(); err != nil {
		t.Fatal(err)
	}
	got, err := r.Read()
	var parseErr *ParseError
	if got != nil || !errors.As(err, &parseErr) || !errors.Is(err, ErrFieldLimit) || parseErr.StartLine != 2 || parseErr.Line != 3 || parseErr.Column != 6 {
		t.Fatalf("reused record=%q error=%+v", got, err)
	}
	for _, limit := range []int{0, -1} {
		r = NewReader(strings.NewReader(strings.Repeat("x,", 32) + "x"))
		r.MaxFieldsPerRecord = limit
		if got, err := r.Read(); err != nil || len(got) != 33 {
			t.Fatalf("unlimited=%d: fields=%d error=%v", limit, len(got), err)
		}
	}
}

func TestReaderMatchesStockGrammar(t *testing.T) {
	inputs := []string{
		"", "\n\r\n", "a,b,", "a,b\rc,d", "a,b\r", "a,b\r\nc,d\r\n",
		"\ufeffRenée,東京\n", "a,b\nx\n", "\"a\r\nb\",c", "\"escaped \"\"quote\"\"\",last",
		"a,\"unfinished\n", "a,b\"c", "\"a\"z,b", "a,\xff\n", "#comment\na,b\n",
		"\"" + strings.Repeat("x", 4094) + "\"\"z\",tail\r\n",
		strings.Repeat("a,b\n", 17), "  a, \"b\"\n", "a§b§\"c§d\"\n",
	}
	// Exhaust short combinations so malformed quotes and line endings are deterministic.
	alphabet := []byte{'a', ',', '"', '\r', '\n', ';', '\t'}
	var add func(string, int)
	add = func(prefix string, remaining int) {
		inputs = append(inputs, prefix)
		if remaining > 0 {
			for _, b := range alphabet {
				add(prefix+string(b), remaining-1)
			}
		}
	}
	add("", 4)
	for _, config := range []struct {
		comma, comment rune
		fields         int
		lazy, trim     bool
	}{
		{',', 0, -1, false, false}, {',', '#', 0, false, true},
		{';', 0, 2, false, false}, {'\t', 0, -1, true, true},
		{'§', 0, -1, false, false}, {',', 0, -1, true, false},
		{'"', 0, -1, false, false}, {',', ',', -1, false, false},
	} {
		for _, input := range inputs {
			for _, limit := range []int{0, 8} {
				stock := csv.NewReader(strings.NewReader(input))
				stock.Comma, stock.Comment, stock.FieldsPerRecord = config.comma, config.comment, config.fields
				stock.LazyQuotes, stock.TrimLeadingSpace, stock.ReuseRecord = config.lazy, config.trim, true
				local := NewReader(strings.NewReader(input))
				local.Comma, local.Comment, local.FieldsPerRecord = config.comma, config.comment, config.fields
				local.LazyQuotes, local.TrimLeadingSpace, local.ReuseRecord = config.lazy, config.trim, true
				local.MaxFieldsPerRecord = limit
				for {
					want, wantErr := stock.Read()
					got, gotErr := local.Read()
					if !slices.Equal(got, want) || (got == nil) != (want == nil) || !sameParseError(gotErr, wantErr) || local.InputOffset() != stock.InputOffset() || local.FieldsPerRecord != stock.FieldsPerRecord {
						t.Fatalf("input=%q config=%+v limit=%d: local=(%q, %v) stock=(%q, %v)", input, config, limit, got, gotErr, want, wantErr)
					}
					for i := range got {
						line, col := local.FieldPos(i)
						wantLine, wantCol := stock.FieldPos(i)
						if line != wantLine || col != wantCol {
							t.Fatalf("input=%q field=%d: position=(%d,%d) want=(%d,%d)", input, i, line, col, wantLine, wantCol)
						}
					}
					if gotErr != nil {
						break
					}
				}
			}
		}
	}
}

func sameParseError(local, stock error) bool {
	if local == nil || stock == nil {
		return local == nil && stock == nil
	}
	if local.Error() != stock.Error() || errors.Is(local, io.EOF) != errors.Is(stock, io.EOF) {
		return false
	}
	var got *ParseError
	var want *csv.ParseError
	if errors.As(local, &got) != errors.As(stock, &want) {
		return false
	}
	if got != nil {
		return got.StartLine == want.StartLine && got.Line == want.Line && got.Column == want.Column &&
			errors.Is(local, ErrQuote) == errors.Is(stock, csv.ErrQuote) &&
			errors.Is(local, ErrBareQuote) == errors.Is(stock, csv.ErrBareQuote) &&
			errors.Is(local, ErrFieldCount) == errors.Is(stock, csv.ErrFieldCount)
	}
	return true
}
