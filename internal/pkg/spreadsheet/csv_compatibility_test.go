package spreadsheet

import (
	"strings"
	"testing"
)

func TestDelimitedImportRetainsItsFieldAndQuoteProfile(t *testing.T) {
	for _, filename := range []string{"contacts.csv", "contacts.tsv", "contacts.txt"} {
		sep := ","
		if strings.HasSuffix(filename, ".tsv") {
			sep = "\t"
		}
		body := strings.Repeat("x"+sep, 4096) + "bare\"quote\r\nshort\r\n"
		rows, kind, err := Parse(strings.NewReader(body), filename, 0)
		if err != nil || kind != "csv" || len(rows) != 2 || len(rows[0]) != 4097 || rows[0][4096] != "bare\"quote" || len(rows[1]) != 1 {
			t.Fatalf("%s: kind=%s rows=%d error=%v", filename, kind, len(rows), err)
		}
	}
}
