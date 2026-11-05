package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/warmbly/warmbly/internal/app/orgtransfer"
)

func runOrgAttachmentsPreflight(ctx context.Context, args []string) error {
	fs := newFlagSet("org attachments-preflight")
	file := fs.String("file", "", "immutable local workspace archive to read (required; no database or network)")
	after := fs.Int("after", 0, "skip this many attachment rows, using next_after from the previous page")
	limit := fs.Int("limit", 25, "maximum attachment rows to inspect (1 to 100)")
	digest := fs.String("expect-sha256", "", "archive_sha256 from the previous page (required with --after)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--file is required")
	}
	pathInfo, err := os.Stat(*file)
	if err != nil {
		return err
	}
	if !pathInfo.Mode().IsRegular() {
		return errors.New("--file must name a regular local archive")
	}
	f, err := openInventoryFile(*file)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("--file must name a regular local archive")
	}
	if !os.SameFile(pathInfo, info) {
		return errors.New("archive changed before inventory; retry with an immutable file")
	}
	report, err := orgtransfer.InventoryAttachments(ctx, &fileArchive{f: f, size: info.Size()}, orgtransfer.AttachmentInventoryOptions{
		After: *after, Limit: *limit, SHA256: *digest,
	})
	if err != nil {
		return err
	}
	finalInfo, err := f.Stat()
	if err != nil {
		return err
	}
	if finalInfo.Size() != info.Size() || !finalInfo.ModTime().Equal(info.ModTime()) {
		return errors.New("archive changed during inventory; discard this page")
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
