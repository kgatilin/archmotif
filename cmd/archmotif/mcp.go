package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kgatilin/archmotif/internal/mcpserver"
)

// httpListenAddrSink is a test-only hook. When non-nil, the chosen listen
// address (after net.Listen resolves wildcards / :0) is sent here exactly
// once, non-blocking. Production code leaves this nil; tests set it to
// avoid scraping stderr (which is racy under -race).
var httpListenAddrSink chan<- string

// runMCP dispatches the `archmotif mcp <subcommand>` family. For now the only
// subcommand is `serve`, which launches the stdio MCP server (or, with
// --http, an HTTP server exposing the same tools).
//
// Wiring into Claude Code (stdio):
//
//	claude mcp add archmotif -- archmotif mcp serve
//
// Wiring into an HTTP client (e.g. a TUI/browser, #71):
//
//	archmotif mcp serve --http 127.0.0.1:7150
//
// (A bare `:7150` is auto-bound to 127.0.0.1 — pass `0.0.0.0:7150` to
// expose to other hosts. There is no auth; do so deliberately.)
func runMCP(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "archmotif mcp: missing subcommand (expected 'serve')")
		return 2
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "serve":
		return runMCPServe(rest, stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "archmotif mcp: unknown subcommand %q\n", sub)
		return 2
	}
}

// runMCPServe parses flags for `archmotif mcp serve` and runs either the
// stdio MCP server (default) or an HTTP server (when --http is set).
//
// Part 1/3 of #68 (live graph mutation transport). This call only serves the
// MCP tool set; WebSocket pushes and mutation broadcast land in #71 parts 2
// and 3.
func runMCPServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("archmotif mcp serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "workspace root for archmotif graphs (default: $ARCHMOTIF_HOME or ~/.archmotif)")
	httpAddr := fs.String("http", "", "listen address for HTTP transport (e.g. '127.0.0.1:7150'; bare ':7150' is auto-bound to 127.0.0.1 — pass '0.0.0.0:7150' to expose); when empty, runs stdio")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: archmotif mcp serve [-root DIR] [--http ADDR]")
		_, _ = fmt.Fprintln(stderr)
		_, _ = fmt.Fprintln(stderr, "Launches a stdio MCP server (default) or an HTTP server (--http) exposing the archmotif graph tools.")
		_, _ = fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	workspace, err := resolveMCPRoot(*root)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif mcp serve: %v\n", err)
		return 1
	}

	// Honour the archmotif CLI version reported in MCP metadata.
	mcpserver.Version = version

	svc := mcpserver.NewService(workspace)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *httpAddr != "" {
		return runMCPServeHTTP(ctx, svc, *httpAddr, stdout, stderr)
	}

	if err := mcpserver.Serve(ctx, svc, os.Stdin, stdout); err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintf(stderr, "archmotif mcp serve: %v\n", err)
		return 1
	}
	return 0
}

// runMCPServeHTTP starts the HTTP transport (#71 part 1/3). Returns 0 once
// ctx is cancelled (SIGINT/SIGTERM) and the server has shut down cleanly.
//
// The chosen listen address is echoed on stderr so callers can grab a random
// port by passing `:0` and parsing the line. Tests can also subscribe to the
// httpListenAddrSink hook to avoid the stderr race.
//
// Security: a bare `:PORT` addr is treated as `127.0.0.1:PORT` so the default
// is loopback-only. The HTTP transport exposes write-capable tools with no
// auth, so binding `0.0.0.0` must be an explicit operator choice.
func runMCPServeHTTP(ctx context.Context, svc *mcpserver.Service, addr string, stdout, stderr io.Writer) int {
	resolvedAddr := addr
	if strings.HasPrefix(addr, ":") {
		resolvedAddr = "127.0.0.1" + addr
	}
	ln, err := net.Listen("tcp", resolvedAddr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "archmotif mcp serve: listen %q: %v\n", resolvedAddr, err)
		return 1
	}
	if sink := httpListenAddrSink; sink != nil {
		select {
		case sink <- ln.Addr().String():
		default:
		}
	}
	if host, _, splitErr := net.SplitHostPort(ln.Addr().String()); splitErr == nil {
		if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
			_, _ = fmt.Fprintf(stderr,
				"archmotif mcp serve: WARNING: HTTP server bound to %s with no auth and write-capable tools — bind to 127.0.0.1 unless you mean it\n",
				ln.Addr().String())
		}
	}
	_, _ = fmt.Fprintf(stderr, "archmotif mcp serve: HTTP listening on %s\n", ln.Addr().String())

	srv := &http.Server{
		Handler:           mcpserver.NewHTTPHandler(svc),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		errs <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_, _ = fmt.Fprintf(stderr, "archmotif mcp serve: shutdown: %v\n", err)
			return 1
		}
		return 0
	case err := <-errs:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			_, _ = fmt.Fprintf(stderr, "archmotif mcp serve: %v\n", err)
			return 1
		}
		return 0
	}
}

// resolveMCPRoot decides where the archmotif workspace lives. Priority:
//  1. explicit -root flag
//  2. $ARCHMOTIF_HOME env var
//  3. $HOME/.archmotif (created lazily on first write)
func resolveMCPRoot(flagRoot string) (string, error) {
	if flagRoot != "" {
		abs, err := filepath.Abs(flagRoot)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	if env := os.Getenv("ARCHMOTIF_HOME"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve $HOME: %w", err)
	}
	return filepath.Join(home, ".archmotif"), nil
}
