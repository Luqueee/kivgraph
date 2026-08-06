package mcp

import (
	"context"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/version"
)

func TestServerInitializesWithIdentityAndCapabilities(t *testing.T) {
	ctx := context.Background()
	server := NewServer()
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	defer serverSession.Close()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "test-client",
		Version: "0.0.1",
	}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	defer clientSession.Close()

	result := clientSession.InitializeResult()
	if result == nil {
		t.Fatal("InitializeResult() = nil")
	}
	if result.ServerInfo == nil {
		t.Fatal("InitializeResult().ServerInfo = nil")
	}
	if result.ServerInfo.Name != serverName {
		t.Fatalf("server name = %q, want %q", result.ServerInfo.Name, serverName)
	}
	if result.ServerInfo.Version != version.Value {
		t.Fatalf("server version = %q, want %q", result.ServerInfo.Version, version.Value)
	}
	if result.Capabilities == nil {
		t.Fatal("InitializeResult().Capabilities = nil")
	}
}
