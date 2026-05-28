package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// SheetsClient is the Google Sheets v4 wrapper Warmbly uses for lead I/O.
// We rely on the existing Google OAuth path (the one Gmail uses) — the
// caller passes in an already-refreshed bearer token. Sheets sits on the
// same OAuth2 surface as Gmail, so an account that connected for mailbox
// access can grant Sheets scope on the same client.
type SheetsClient struct {
	bearerToken string
	http        *http.Client
}

func NewSheetsClient(bearerToken string) *SheetsClient {
	return &SheetsClient{
		bearerToken: bearerToken,
		http:        &http.Client{Timeout: 15 * time.Second},
	}
}

// SheetRange describes a contiguous block of cells using A1 notation.
type SheetRange struct {
	SheetID string `json:"sheet_id"`
	A1Range string `json:"a1_range"`
}

// SheetMeta is what GetSpreadsheet returns. Only the bits the dashboard
// uses are modelled.
type SheetMeta struct {
	SheetID string `json:"sheet_id"`
	Title   string `json:"title"`
	Tabs    []struct {
		Title string `json:"title"`
		Index int    `json:"index"`
	} `json:"tabs"`
}

// GetSpreadsheet pulls the sheet's metadata so the dashboard can render
// "connected to <title>". Also a cheap "is this token still valid?" check.
func (c *SheetsClient) GetSpreadsheet(ctx context.Context, sheetID string) (*SheetMeta, error) {
	endpoint := "https://sheets.googleapis.com/v4/spreadsheets/" + url.PathEscape(sheetID) +
		"?fields=spreadsheetId,properties.title,sheets.properties(title,index)"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("sheets get HTTP %d: %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		SpreadsheetID string `json:"spreadsheetId"`
		Properties    struct {
			Title string `json:"title"`
		} `json:"properties"`
		Sheets []struct {
			Properties struct {
				Title string `json:"title"`
				Index int    `json:"index"`
			} `json:"properties"`
		} `json:"sheets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	meta := &SheetMeta{SheetID: parsed.SpreadsheetID, Title: parsed.Properties.Title}
	for _, s := range parsed.Sheets {
		meta.Tabs = append(meta.Tabs, struct {
			Title string `json:"title"`
			Index int    `json:"index"`
		}{Title: s.Properties.Title, Index: s.Properties.Index})
	}
	return meta, nil
}

// ReadValues fetches the cell values for a given A1 range. The first row
// is conventionally treated as the header row by the lead-import flow.
func (c *SheetsClient) ReadValues(ctx context.Context, sheetID, a1Range string) ([][]string, error) {
	endpoint := "https://sheets.googleapis.com/v4/spreadsheets/" + url.PathEscape(sheetID) +
		"/values/" + url.PathEscape(a1Range)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("sheets read HTTP %d: %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Values [][]string `json:"values"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed.Values, nil
}

// AppendValues writes new rows to the bottom of the sheet. Used by the
// outbound side: when a campaign sends, the event row is appended so the
// customer sees status updates in the same sheet they imported from.
func (c *SheetsClient) AppendValues(ctx context.Context, sheetID, a1Range string, rows [][]string) error {
	endpoint := "https://sheets.googleapis.com/v4/spreadsheets/" + url.PathEscape(sheetID) +
		"/values/" + url.PathEscape(a1Range) +
		":append?valueInputOption=RAW&insertDataOption=INSERT_ROWS"
	payload := map[string]any{"values": rows}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("sheets append HTTP %d: %s", resp.StatusCode, string(respBody))
}
