package netbind

import (
	"crypto/tls"
	"net"
	"testing"
	"time"
)

func TestDialer_ExplicitBindIP(t *testing.T) {
	want := &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}
	d := Dialer(want)
	if d.LocalAddr != want {
		t.Fatalf("LocalAddr = %v, want %v", d.LocalAddr, want)
	}
	if d.Timeout != defaultTimeout {
		t.Fatalf("Timeout = %v, want %v", d.Timeout, defaultTimeout)
	}
}

func TestDialer_NilFallsBackToEnv(t *testing.T) {
	// FromEnv is cached via sync.Once so we can't manipulate the env mid-test;
	// just verify the call doesn't panic and returns a usable dialer.
	d := Dialer(nil)
	if d == nil {
		t.Fatal("Dialer(nil) returned nil")
	}
	if d.Timeout == 0 {
		t.Fatal("dialer should have a default timeout")
	}
}

func TestTLSDialer_WrapsNetDialer(t *testing.T) {
	bindIP := &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}
	cfg := &tls.Config{ServerName: "example.com"}
	td := TLSDialer(bindIP, cfg)
	if td.Config != cfg {
		t.Fatal("tls.Dialer.Config not propagated")
	}
	if td.NetDialer == nil {
		t.Fatal("tls.Dialer.NetDialer is nil")
	}
	if td.NetDialer.LocalAddr != bindIP {
		t.Fatal("tls.Dialer.NetDialer.LocalAddr not propagated")
	}
}

func TestDefaultTimeoutIsReasonable(t *testing.T) {
	if defaultTimeout < time.Second || defaultTimeout > time.Minute {
		t.Fatalf("defaultTimeout = %v looks wrong", defaultTimeout)
	}
}
