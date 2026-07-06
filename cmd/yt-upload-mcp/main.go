// yt-upload-mcp is an MCP stdio server that publishes videos/Shorts to a
// fixed set of pre-configured YouTube channels via the YouTube Data API v3.
//
// Usage:
//
//	yt-upload-mcp --config /path/to/config.json
//
// The config path can also come from the YT_UPLOAD_MCP_CONFIG env var.
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"yt-mcp/internal/config"
	"yt-mcp/internal/mcptool"
	"yt-mcp/internal/store"
	"yt-mcp/internal/uploader"
)

func main() {
	// stdout carries the MCP JSON-RPC protocol; anything written there would
	// corrupt it. All diagnostics go to stderr.
	log.SetOutput(os.Stderr)
	log.SetPrefix("yt-upload-mcp: ")
	log.SetFlags(0)

	defaultCfg := os.Getenv("YT_UPLOAD_MCP_CONFIG")
	if defaultCfg == "" {
		defaultCfg = "config.json"
	}
	cfgPath := flag.String("config", defaultCfg, "path to the JSON config file")
	envFile := flag.String("env-file", "", "optional dotenv file of KEY=VALUE secrets (e.g. YT_CLIENT_ID, YT_REFRESH_TOKEN_*), loaded before config; file values override the shell environment")
	flag.Parse()

	// Load the env file (if any) BEFORE config.Load so its ${VAR} references
	// resolve against it. File-wins precedence — see config.LoadEnvFile.
	if *envFile != "" {
		if err := config.LoadEnvFile(*envFile); err != nil {
			log.Fatal(err)
		}
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(cfg.StateDir)
	if err != nil {
		log.Fatalf("open state dir: %v", err)
	}
	up := uploader.New(cfg, st)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "yt-upload-mcp",
		Title:   "YouTube Upload MCP",
		Version: "1.0.0",
	}, nil)
	mcptool.Register(server, up, cfg)

	log.Printf("serving %d channel(s) over stdio; state dir %s", len(cfg.Channels), cfg.StateDir)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
