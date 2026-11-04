// Package spreadsheet reads an uploaded CSV, TSV or XLSX into rows of cells.
// Every error it returns is written for the person who uploaded the file.
package spreadsheet

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/textcontent"
	"github.com/xuri/excelize/v2"
)

// XLSX decompression budgets. An import is text, so even a very large one is
// tens of megabytes; these are generous for that and far below what a zip bomb needs.
const (
	xlsxUnzipLimitBytes    = 512 << 20 // 512 MiB total uncompressed
	xlsxUnzipXMLLimitBytes = 64 << 20  // 64 MiB for any single XML part
)

// Parse returns the file's rows and its format ("csv" or "xlsx"). An XLSX is
// read up to maxRows rows so the caller can still tell "too many" from "at the
// limit" by asking for one past its cap.
//
// It recovers from a panic in the parser. The XLSX reader is a third-party
// parser of a zip of XML written by whoever uploaded the file, and it carries
// at least one open advisory with no fix available (a negative shared-string
// index panics). A malformed workbook is the caller's problem and should read
// as one, not as an instance fault that pages the error tracker.
func Parse(r io.Reader, filename string, maxRows int) (rows [][]string, kind string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			rows, kind = nil, ""
			err = errors.New("this file could not be read as a spreadsheet; export it again from your spreadsheet application and retry")
		}
	}()
	return parse(r, filename, maxRows)
}

func parse(r io.Reader, filename string, maxRows int) ([][]string, string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".csv", ".tsv", ".txt", "":
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, "csv", errors.New("failed to read the file: " + err.Error())
		}
		if err := textcontent.Validate(data); err != nil {
			return nil, "csv", err
		}
		reader := csv.NewReader(bytes.NewReader(data))
		reader.FieldsPerRecord = -1 // tolerate ragged rows; callers pad
		reader.LazyQuotes = true
		if ext == ".tsv" {
			reader.Comma = '\t'
		} else {
			reader.Comma = Delimiter(data)
		}
		rows, err := reader.ReadAll()
		if err != nil {
			return nil, "csv", errors.New("failed to parse CSV: " + err.Error())
		}
		return rows, "csv", nil
	case ".xlsx", ".xlsm":
		// An XLSX is a zip of XML, so its uncompressed size is unrelated to the
		// upload cap. excelize defaults to a 16 GB unzip budget, and GetRows
		// materialises the whole sheet before any row cap applies, so bound the
		// decompression, then stream the rows and stop at the cap.
		f, err := excelize.OpenReader(r, excelize.Options{
			UnzipSizeLimit:    xlsxUnzipLimitBytes,
			UnzipXMLSizeLimit: xlsxUnzipXMLLimitBytes,
		})
		if err != nil {
			return nil, "xlsx", errors.New("failed to parse XLSX: " + err.Error())
		}
		defer f.Close()
		sheetName := f.GetSheetName(f.GetActiveSheetIndex())
		if sheetName == "" {
			names := f.GetSheetList()
			if len(names) == 0 {
				return nil, "xlsx", errors.New("workbook has no sheets")
			}
			sheetName = names[0]
		}
		it, err := f.Rows(sheetName)
		if err != nil {
			return nil, "xlsx", errors.New("failed to read XLSX rows: " + err.Error())
		}
		defer it.Close()

		rows := make([][]string, 0, 256)
		for it.Next() {
			cols, cerr := it.Columns()
			if cerr != nil {
				return nil, "xlsx", errors.New("failed to read XLSX rows: " + cerr.Error())
			}
			rows = append(rows, cols)
			if maxRows > 0 && len(rows) >= maxRows {
				break
			}
		}
		if err := it.Error(); err != nil {
			return nil, "xlsx", errors.New("failed to read XLSX rows: " + err.Error())
		}
		return rows, "xlsx", nil
	}
	return nil, "", errors.New("unsupported file type: " + ext)
}

// Delimiter is the separator a delimited file uses, read off its first line:
// a comma, or the semicolon spreadsheet apps write where the comma is the
// decimal mark, or a tab. Separators inside quotes do not count.
func Delimiter(data []byte) rune {
	counts := map[rune]int{}
	quoted := false
	for _, r := range string(bytes.TrimPrefix(data, []byte("\ufeff"))) {
		if r == '"' {
			quoted = !quoted
			continue
		}
		if !quoted && (r == '\n' || r == '\r') {
			if counts[','] > 0 || counts[';'] > 0 || counts['\t'] > 0 {
				break
			}
			continue
		}
		if !quoted && (r == ',' || r == ';' || r == '\t') {
			counts[r]++
		}
	}
	best := ','
	for _, r := range []rune{';', '\t'} {
		if counts[r] > counts[best] {
			best = r
		}
	}
	return best
}
