package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/quay/release-readiness/internal/syncstatus"
)

func newTestRunServer(addr string) *Server {
	return New(nil, addr, "", "", nil, syncstatus.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestRunReturnsListenError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	done := make(chan error, 1)
	go func() { done <- newTestRunServer(ln.Addr().String()).Run(context.Background()) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil on an occupied port")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return on an occupied port")
	}
}

func TestRunCancelShutsDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- newTestRunServer(addr).Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never listened: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
