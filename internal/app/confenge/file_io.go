package confenge

import (
	"fmt"
	"io"
	"os"
)

func readFileLimited(path string, maxBytes int64) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if maxBytes <= 0 {
		maxBytes = DefaultMaxPayloadBytes
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("payload exceeds max size of %d bytes", maxBytes)
	}
	return data, nil
}
