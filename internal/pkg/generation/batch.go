package generation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/openai/openai-go/v2"
)

// BatchRequest is one warmup thread to generate via the OpenAI Batch API. The
// CustomID round-trips through the batch so results can be mapped back to their
// theme (the batch output is unordered and may drop failed lines).
type BatchRequest struct {
	CustomID    string
	Theme       string
	Model       string
	MaxMessages int
}

// BatchResult is one parsed line from a completed batch's output file. Exactly
// one of Conversation or Err is set per line; Err carries a per-line failure so
// a single bad response never fails the whole ingest.
type BatchResult struct {
	CustomID     string
	Theme        string
	Conversation *Conversation
	Err          string
}

// BatchCounts mirrors the OpenAI batch request_counts object.
type BatchCounts struct {
	Completed int
	Failed    int
	Total     int
}

// batchInputLine is one line of the Batch API JSONL input file. The body is the
// same chat-completion request the sync path sends, so sync and batch produce
// identical threads.
type batchInputLine struct {
	CustomID string                         `json:"custom_id"`
	Method   string                         `json:"method"`
	URL      string                         `json:"url"`
	Body     openai.ChatCompletionNewParams `json:"body"`
}

// batchOutputLine is one line of the Batch API JSONL output file.
type batchOutputLine struct {
	CustomID string `json:"custom_id"`
	Response *struct {
		StatusCode int                   `json:"status_code"`
		Body       openai.ChatCompletion `json:"body"`
	} `json:"response"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

// SubmitBatch uploads a JSONL of chat-completion requests as a batch input file
// and creates a batch over the /v1/chat/completions endpoint. It returns the
// batch ID and the uploaded input file ID. completionWindow is the Batch API
// processing window (only "24h" is currently accepted by OpenAI; empty defaults
// to "24h").
func (c *GenerationClient) SubmitBatch(ctx context.Context, requests []BatchRequest, completionWindow string) (batchID, inputFileID string, err error) {
	if len(requests) == 0 {
		return "", "", fmt.Errorf("submit batch: no requests")
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range requests {
		body := buildConversationParams(r.Theme, r.Model, normalizeMaxMessages(r.MaxMessages))
		line := batchInputLine{
			CustomID: r.CustomID,
			Method:   "POST",
			URL:      "/v1/chat/completions",
			Body:     body,
		}
		if err := enc.Encode(line); err != nil {
			return "", "", fmt.Errorf("submit batch: encode line %s: %w", r.CustomID, err)
		}
	}

	file, err := c.client.Files.New(ctx, openai.FileNewParams{
		File:    namedReader{Reader: bytes.NewReader(buf.Bytes()), name: "warmup_batch.jsonl"},
		Purpose: openai.FilePurposeBatch,
	})
	if err != nil {
		return "", "", fmt.Errorf("submit batch: upload input file: %w", err)
	}

	window := openai.BatchNewParamsCompletionWindow24h
	if completionWindow != "" {
		window = openai.BatchNewParamsCompletionWindow(completionWindow)
	}

	batch, err := c.client.Batches.New(ctx, openai.BatchNewParams{
		CompletionWindow: window,
		Endpoint:         openai.BatchNewParamsEndpointV1ChatCompletions,
		InputFileID:      file.ID,
	})
	if err != nil {
		return "", "", fmt.Errorf("submit batch: create batch: %w", err)
	}

	return batch.ID, file.ID, nil
}

// GetBatch returns the current status, output file ID (when completed), and
// request counts for a batch.
func (c *GenerationClient) GetBatch(ctx context.Context, batchID string) (status, outputFileID string, counts BatchCounts, err error) {
	batch, err := c.client.Batches.Get(ctx, batchID)
	if err != nil {
		return "", "", BatchCounts{}, err
	}
	counts = BatchCounts{
		Completed: int(batch.RequestCounts.Completed),
		Failed:    int(batch.RequestCounts.Failed),
		Total:     int(batch.RequestCounts.Total),
	}
	return string(batch.Status), batch.OutputFileID, counts, nil
}

// CancelBatch requests cancellation of an in-flight batch.
func (c *GenerationClient) CancelBatch(ctx context.Context, batchID string) error {
	_, err := c.client.Batches.Cancel(ctx, batchID)
	return err
}

// FetchBatchResults downloads a completed batch's output file and parses each
// JSONL line back into a Conversation. Per-line failures (HTTP error, malformed
// body, JSON parse error) are reported on the individual BatchResult rather than
// failing the whole fetch, so a few bad lines don't discard the good ones.
func (c *GenerationClient) FetchBatchResults(ctx context.Context, outputFileID string) ([]BatchResult, error) {
	if outputFileID == "" {
		return nil, fmt.Errorf("fetch batch results: empty output file id")
	}
	resp, err := c.client.Files.Content(ctx, outputFileID)
	if err != nil {
		return nil, fmt.Errorf("fetch batch results: download output file: %w", err)
	}
	defer resp.Body.Close()

	var out []BatchResult
	scanner := bufio.NewScanner(resp.Body)
	// Output bodies can be large; raise the line buffer well above the default 64KiB.
	scanner.Buffer(make([]byte, 0, 1024*1024), 8*1024*1024)
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		out = append(out, parseBatchOutputLine(raw))
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("fetch batch results: read output file: %w", err)
	}
	return out, nil
}

// parseBatchOutputLine decodes one output JSONL line, tolerating per-line errors.
func parseBatchOutputLine(raw string) BatchResult {
	var line batchOutputLine
	if err := json.Unmarshal([]byte(raw), &line); err != nil {
		return BatchResult{Err: fmt.Sprintf("parse output line: %v", err)}
	}
	res := BatchResult{CustomID: line.CustomID}
	if line.Error != nil && line.Error.Message != "" {
		res.Err = line.Error.Message
		return res
	}
	if line.Response == nil {
		res.Err = "missing response in output line"
		return res
	}
	if line.Response.StatusCode < 200 || line.Response.StatusCode >= 300 {
		res.Err = fmt.Sprintf("response status %d", line.Response.StatusCode)
		return res
	}
	if len(line.Response.Body.Choices) == 0 {
		res.Err = "response returned no choices"
		return res
	}
	var conv Conversation
	if err := json.Unmarshal([]byte(line.Response.Body.Choices[0].Message.Content), &conv); err != nil {
		res.Err = fmt.Sprintf("parse conversation: %v", err)
		return res
	}
	res.Conversation = &conv
	return res
}

// namedReader adapts a reader so the multipart upload carries a filename, which
// the Files endpoint requires for the JSONL input.
type namedReader struct {
	io.Reader
	name string
}

func (n namedReader) Name() string { return n.name }
