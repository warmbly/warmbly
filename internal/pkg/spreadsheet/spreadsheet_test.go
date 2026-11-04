package spreadsheet

import (
	"bytes"
	"github.com/xuri/excelize/v2"
	"strings"
	"testing"
)

func TestDelimiterFollowsTheHeader(t *testing.T) {
	cases := map[string]rune{
		"email,password\na@x.test,p":                   ',',
		"email;password;name\na@x.test;p;A":            ';',
		"\ufeffemail;password\na@x.test;p":             ';',
		"email\tpassword\na@x.test\tp":                 '\t',
		`"Smith, Ann";email;password` + "\n" + "x;y;z": ';',
		"email\na@x.test":                              ',',
		"\n\nemail;password\na;b":                      ';',
	}
	for in, want := range cases {
		if got := Delimiter([]byte(in)); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestParseReadsASemicolonFile(t *testing.T) {
	rows, kind, err := Parse(strings.NewReader("email;password\na@x.test;secret, with comma\n"), "boxes.csv", 10)
	if err != nil || kind != "csv" || len(rows) != 2 || len(rows[1]) != 2 || rows[1][1] != "secret, with comma" {
		t.Fatalf("rows = %q, %s, %v", rows, kind, err)
	}
}

func TestDelimitedTextClassificationPreservesSupportedRows(t *testing.T) {
	for _, filename := range []string{"contacts.csv", "contacts.txt", "contacts"} {
		data := "\ufeffemail;name\r\na@example.test;Renée 東京\r\nb@example.test\r\n"
		rows, kind, err := Parse(strings.NewReader(data), filename, 10)
		if err != nil || kind != "csv" || len(rows) != 3 || rows[0][0] != "\ufeffemail" || rows[1][1] != "Renée 東京" || len(rows[2]) != 1 {
			t.Fatalf("%s: rows=%q kind=%s err=%v", filename, rows, kind, err)
		}
	}
	rows, _, err := Parse(strings.NewReader("name\tcity\n\"Renée\nSmith\"\t東京\n"), "people.tsv", 10)
	if err != nil || len(rows) != 2 || rows[1][0] != "Renée\nSmith" || rows[1][1] != "東京" {
		t.Fatalf("TSV: %q %v", rows, err)
	}
	for _, data := range []string{"MZ\x00binary", "name,\xff", "name,\x1bescape"} {
		if _, _, err := Parse(strings.NewReader(data), "contacts.csv", 10); err == nil {
			t.Fatal("accepted binary text")
		}
	}
}

func TestWorkbookImportProfilesRemainSupported(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetCellValue("Sheet1", "A1", "Renée 東京"); err != nil {
		t.Fatal(err)
	}
	data, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"contacts.xlsx", "contacts.xlsm"} {
		rows, kind, err := Parse(bytes.NewReader(data.Bytes()), filename, 2)
		if err != nil || kind != "xlsx" || len(rows) != 1 || rows[0][0] != "Renée 東京" {
			t.Fatalf("%s: %q %s %v", filename, rows, kind, err)
		}
	}
}
