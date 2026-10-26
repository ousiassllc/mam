// Package serverinfo holds get_server_info: the one tool that needs no service, to see that the connection
// and the token work.
package serverinfo

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Register adds get_server_info. The service layer is not used.
func Register(r *mcpkit.Registrar, _ *service.Services) {
	mcpkit.Read(r, "get_server_info",
		"mam MCP サーバーの情報（バージョン・待ち受けアドレス・ツール数）/ Server version, listen address and number of tools.",
		func(_ context.Context, _ Input) (Output, error) {
			return Output{Name: "mam", Version: r.Version, ListenAddr: r.ListenAddr(), ToolCount: len(r.ToolNames())}, nil
		})
}

// Input has no field.
type Input struct{}

// Output is the server's identity.
type Output struct {
	Name       string `json:"name"`
	Version    string `json:"version" jsonschema:"mam のバージョン（開発ビルドは dev）"`
	ListenAddr string `json:"listen_addr" jsonschema:"MCP の待ち受けアドレス（host:port）"`
	ToolCount  int    `json:"tool_count" jsonschema:"登録されているツールの数"`
}
