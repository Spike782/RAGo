package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	mcpclient "ai-chat/common/mcp/client"
	mcpserver "ai-chat/common/mcp/server"

	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	mode := flag.String("mode", "", "run mode: server or client")
	httpAddr := flag.String("http-addr", ":8082", "HTTP server address")
	city := flag.String("city", "", "city name for weather query")
	tool := flag.String("tool", "get_weather", "tool name: get_weather|get_weather_forecast|web_search|web_fetch|translate_text")
	days := flag.Int("days", 3, "forecast days for get_weather_forecast")
	query := flag.String("query", "", "query for web_search")
	limit := flag.Int("limit", 5, "limit for web_search")
	targetURL := flag.String("url", "", "url for web_fetch")
	maxChars := flag.Int("max-chars", 4000, "max chars for web_fetch")
	text := flag.String("text", "", "text for translate_text")
	sourceLang := flag.String("source-lang", "auto", "source language for translate_text")
	targetLang := flag.String("target-lang", "zh", "target language for translate_text")
	flag.Parse()

	if *mode == "" {
		fmt.Println("Error: -mode is required (server or client)")
		flag.Usage()
		os.Exit(1)
	}

	switch *mode {
	case "server":
		fmt.Println("Starting MCP server...")
		if err := mcpserver.StartServer(*httpAddr); err != nil {
			log.Fatalf("server error: %v", err)
		}
	case "client":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		httpURL := buildMCPURL(*httpAddr)
		mcpClient, err := mcpclient.NewMCPClient(httpURL)
		if err != nil {
			log.Fatalf("create client failed: %v", err)
		}
		defer mcpClient.Close()

		if _, err := mcpClient.Initialize(ctx); err != nil {
			log.Fatalf("initialize failed: %v", err)
		}
		if err := mcpClient.Ping(ctx); err != nil {
			log.Fatalf("health check failed: %v", err)
		}

		var result *mcp.CallToolResult

		switch *tool {
		case "get_weather":
			if *city == "" {
				log.Fatal("-city is required for get_weather")
			}
			result, err = mcpClient.CallWeatherTool(ctx, *city)

		case "get_weather_forecast":
			if *city == "" {
				log.Fatal("-city is required for get_weather_forecast")
			}
			result, err = mcpClient.CallWeatherForecastTool(ctx, *city, *days)

		case "web_search":
			if strings.TrimSpace(*query) == "" {
				log.Fatal("-query is required for web_search")
			}
			result, err = mcpClient.CallWebSearchTool(ctx, *query, *limit)

		case "web_fetch":
			if strings.TrimSpace(*targetURL) == "" {
				log.Fatal("-url is required for web_fetch")
			}
			result, err = mcpClient.CallWebFetchTool(ctx, *targetURL, *maxChars)
		case "translate_text":
			if strings.TrimSpace(*text) == "" {
				log.Fatal("-text is required for translate_text")
			}
			result, err = mcpClient.CallTranslateTool(ctx, *text, *sourceLang, *targetLang)

		default:
			log.Fatalf("unsupported tool: %s", *tool)
		}

		if err != nil {
			log.Fatalf("tool call failed: %v", err)
		}

		fmt.Println("\nTool result:")
		fmt.Println(mcpClient.GetToolResultText(result))
	default:
		fmt.Println("Error: unsupported mode, use server or client")
		os.Exit(1)
	}
}

func buildMCPURL(httpAddr string) string {
	addr := strings.TrimSpace(httpAddr)
	if addr == "" {
		return "http://127.0.0.1:8082/mcp"
	}

	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		addr = strings.TrimRight(addr, "/")
		if strings.HasSuffix(addr, "/mcp") {
			return addr
		}
		return addr + "/mcp"
	}

	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr + "/mcp"
	}

	addr = strings.TrimRight(addr, "/")
	if strings.HasSuffix(addr, "/mcp") {
		return "http://" + addr
	}
	return "http://" + addr + "/mcp"
}
