package spreadsheet

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
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
		"\t\nemail,name\na,b":                          ',',
		",,\nemail;name\na;b":                          ';',
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
	rows, kind, err := Parse(bytes.NewReader(data.Bytes()), "contacts.xlsx", 2)
	if err != nil || kind != "xlsx" || len(rows) != 1 || len(rows[0]) != 1 || rows[0][0] != "Renée 東京" {
		t.Fatalf("%q %s %v", rows, kind, err)
	}
}

func TestMacroEnabledWorkbookImport(t *testing.T) {
	data, err := os.ReadFile("testdata/macro01.xlsm")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	macroType, vba := false, false
	for _, part := range pkg.File {
		if part.Name != "[Content_Types].xml" && part.Name != "xl/vbaProject.bin" {
			continue
		}
		r, err := part.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(r)
		closeErr := r.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("%s: read=%v close=%v", part.Name, readErr, closeErr)
		}
		if part.Name == "[Content_Types].xml" {
			macroType = bytes.Contains(body, []byte(`ContentType="application/vnd.ms-excel.sheet.macroEnabled.main+xml"`))
		} else {
			vba = bytes.HasPrefix(body, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1})
		}
	}
	if !macroType || !vba {
		t.Fatal("fixture must contain a macro-enabled workbook and a VBA project")
	}
	before := bytes.Clone(data)
	rows, kind, err := Parse(bytes.NewReader(data), "contacts.xlsm", 2)
	if err != nil || kind != "xlsx" || len(rows) != 1 || len(rows[0]) != 1 || rows[0][0] != "123" {
		t.Fatalf("%q %s %v", rows, kind, err)
	}
	if !bytes.Equal(data, before) {
		t.Fatal("workbook bytes changed")
	}
}

func TestParseSkipsLeadingBlankRows(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	for cell, v := range map[string]string{"A1": " \ufeff ", "B3": "email", "C3": "name", "B4": "a@example.test", "C4": "Ann"} {
		if err := f.SetCellValue("Sheet1", cell, v); err != nil {
			t.Fatal(err)
		}
	}
	data, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := Parse(bytes.NewReader(data.Bytes()), "contacts.xlsx", 2)
	if err != nil || len(rows) != 2 || len(rows[0]) != 3 || rows[0][1] != "email" || rows[1][1] != "a@example.test" {
		t.Fatalf("xlsx: %q %v", rows, err)
	}

	rows, _, err = Parse(strings.NewReader("\ufeff,,\n   \nemail,name\na@example.test,Ann\n"), "contacts.csv", 10)
	if err != nil || len(rows) != 2 || rows[0][0] != "email" || rows[1][1] != "Ann" {
		t.Fatalf("csv: %q %v", rows, err)
	}

	rows, _, err = Parse(strings.NewReader("\t\n  \nemail,name\na@example.test,Ann\n"), "contacts.csv", 10)
	if err != nil || len(rows) != 2 || len(rows[0]) != 2 || rows[0][1] != "name" {
		t.Fatalf("separator-only lines: %q %v", rows, err)
	}

	rows, _, err = Parse(strings.NewReader(",,\nemail;name\na@example.test;Smith, Ann\n"), "contacts.csv", 10)
	if err != nil || len(rows) != 2 || len(rows[0]) != 2 || rows[0][0] != "email" || rows[1][1] != "Smith, Ann" {
		t.Fatalf("comma line above a semicolon header: %q %v", rows, err)
	}

	rows, _, err = Parse(strings.NewReader(",,\n,,\n"), "contacts.csv", 10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("blank csv: %q %v", rows, err)
	}
}
