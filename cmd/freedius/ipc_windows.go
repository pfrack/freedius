//go:build windows

package main

import (
	"context"
	"fmt"

	"github.com/pfrack/freedius/config"
	"github.com/pfrack/freedius/proxy"
)

type IPCServer struct{}

func NewIPCServer(
	socketPath string,
	bus *proxy.EventBus,
	logSink *proxy.LogSink,
	cfg *config.Config,
	registry *proxy.Registry,
	host string,
	port int,
) *IPCServer {
	return &IPCServer{}
}

func (s *IPCServer) ListenAndServe() error              { return fmt.Errorf("IPC not supported on Windows") }
func (s *IPCServer) Shutdown(ctx context.Context) error { return nil }
