package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

type MCPClient struct {
	c *client.Client
}

func NewMCPClient(httpURL string) (*MCPClient, error) {
	fmt.Println("正在初始化HTTP客户端")
	httpTransport, err := transport.NewStreamableHTTP(httpURL)
	if err != nil {
		return nil, fmt.Errorf("创建HTTP传输失败: %w", err)
	}

	c := client.NewClient(httpTransport)
	return &MCPClient{c: c}, nil
}

func (m *MCPClient) Initialize(ctx context.Context) (*mcp.InitializeResult, error) {
	m.c.OnNotification(func(notification mcp.JSONRPCNotification) {
		fmt.Printf("收到通知: %s\n", notification.Method)
	})

	fmt.Println("正在初始化客户端")
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "MCP-Go Weather Client",
		Version: "1.0.0",
	}
	initRequest.Params.Capabilities = mcp.ClientCapabilities{}

	serverInfo, err := m.c.Initialize(ctx, initRequest)
	if err != nil {
		return nil, fmt.Errorf("初始化失败: %w", err)
	}

	fmt.Printf("连接到服务器: %s (版本 %s)\n", serverInfo.ServerInfo.Name, serverInfo.ServerInfo.Version)

	return serverInfo, nil
}

func (m *MCPClient) Ping(ctx context.Context) error {
	fmt.Println("正在执行健康检查")
	if err := m.c.Ping(ctx); err != nil {
		return fmt.Errorf("健康检查失败: %w", err)
	}
	fmt.Println("服务器正常运行并响应")
	return nil
}

func (m *MCPClient) CallTool(ctx context.Context, toolName string, args map[string]any) (*mcp.CallToolResult, error) {
	CallToolRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	}

	result, err := m.c.CallTool(ctx, CallToolRequest)
	if err != nil {
		return nil, fmt.Errorf("调用工具失败: %w", err)
	}
	return result, nil
}

func (m *MCPClient) CallWeatherTool(ctx context.Context, city string) (*mcp.CallToolResult, error) {
	fmt.Printf("正在查询城市 %s 的天气\n", city)

	callToolRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_weather",
			Arguments: map[string]any{
				"city": city,
			},
		},
	}
	result, err := m.c.CallTool(ctx, callToolRequest)
	if err != nil {
		return nil, fmt.Errorf("调用工具失败: %w", err)
	}

	return result, nil
}

func (m *MCPClient) CallWeatherForecastTool(ctx context.Context, city string, days int) (*mcp.CallToolResult, error) {
	return m.CallTool(ctx, "get_weather_forecast", map[string]any{
		"city": city,
		"days": days,
	})
}

func (m *MCPClient) CallWebSearchTool(ctx context.Context, query string, limit int) (*mcp.CallToolResult, error) {
	return m.CallTool(ctx, "web_search", map[string]any{
		"query": query,
		"limit": limit,
	})
}

func (m *MCPClient) CallWebFetchTool(ctx context.Context, targetURL string, maxChars int) (*mcp.CallToolResult, error) {
	return m.CallTool(ctx, "web_fetch", map[string]any{
		"url":       targetURL,
		"max_chars": maxChars,
	})
}


func (m *MCPClient) GetToolResultText(result *mcp.CallToolResult) string {
	var text string
	for _, content := range result.Content {
		if textContent, ok := content.(mcp.TextContent); ok {
			text += textContent.Text + "\n"
		}
	}
	return text
}

func (m *MCPClient) Close() {
	if m.c != nil {
		m.c.Close()
	}
}
