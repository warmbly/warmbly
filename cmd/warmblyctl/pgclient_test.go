package main

import (
	"io"
	"strings"
	"testing"
)

func TestPickPGMajorPrefersTheServersOwnThenTheNearestNewer(t *testing.T) {
	cases := []struct {
		server    int
		installed []int
		want      int
		ok        bool
	}{
		{16, []int{18, 16, 17}, 16, true},
		{17, []int{16, 17, 18}, 17, true},
		{15, []int{18, 16, 17}, 16, true},
		{19, []int{16, 17, 18}, 0, false},
		{16, nil, 0, false},
	}
	for _, c := range cases {
		got, ok := pickPGMajor(c.server, c.installed)
		if got != c.want || ok != c.ok {
			t.Errorf("server %d with %v: got %d %v, want %d %v", c.server, c.installed, got, ok, c.want, c.ok)
		}
	}
}

// pg18Dump is the head of what pg_dump 18 writes, then data that only looks like a preamble line.
const pg18Dump = `--
-- PostgreSQL database dump
--

\restrict abc123

-- Dumped from database version 16.15
-- Dumped by pg_dump version 18.6

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SELECT pg_catalog.set_config('search_path', '', false);
SET row_security = off;

CREATE TABLE public.notes (body text);

COPY public.notes (body) FROM stdin;
SET transaction_timeout = 0;
\.

\unrestrict abc123
`

func pg16Knows(name string) bool {
	switch name {
	case "statement_timeout", "lock_timeout", "idle_in_transaction_session_timeout", "client_encoding", "row_security":
		return true
	}
	return false
}

func TestWithoutUnknownSettingsCommentsOutOnlyThePreamblesUnknownSettings(t *testing.T) {
	r, skipped, err := withoutUnknownSettings(strings.NewReader(pg18Dump), pg16Knows)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(r)
	want := strings.Replace(pg18Dump, "SET transaction_timeout = 0;\nSET client_encoding",
		"-- skipped, not a setting on this server: SET transaction_timeout = 0;\nSET client_encoding", 1)
	if string(out) != want {
		t.Fatalf("filtered dump:\n%s\nwant:\n%s", out, want)
	}
	if len(skipped) != 1 || skipped[0] != "transaction_timeout" {
		t.Fatalf("skipped %v, want [transaction_timeout]", skipped)
	}
}

func TestWithoutUnknownSettingsLeavesADumpTheServerAcceptsUnchanged(t *testing.T) {
	r, skipped, err := withoutUnknownSettings(strings.NewReader(pg18Dump), func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(r)
	if string(out) != pg18Dump || len(skipped) != 0 {
		t.Fatalf("a server that knows every setting got a changed dump (skipped %v):\n%s", skipped, out)
	}
}
