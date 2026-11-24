package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/teatak/pudding-core/internal/acp"
	"github.com/teatak/pudding-core/internal/home"
)

// MCP clients launch this command with pipes. The desktop owns credentials,
// widget identities and the session router. Only the local STDIO stream is forwarded.
func runMCP(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("puddingd mcp", flag.ContinueOnError)
	host := flags.Bool("host", false, "ACP host-owned connection")
	homeDir := flags.String("home", "", "Pudding data directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: puddingd mcp [--home directory]")
	}
	if *host {
		return acp.ForwardHostMCP(os.Getenv("PUDDING_MCP_HOST_URL"), os.Getenv("PUDDING_MCP_HOST_TOKEN"), stdin, stdout)
	}
	dir, err := home.Resolve(*homeDir)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "mcp", "widget-mcp.json"))
	if err != nil {
		return fmt.Errorf("open Pudding Desktop first: %w", err)
	}
	var endpoint struct{ URL, Token string }
	if err = json.Unmarshal(raw, &endpoint); err != nil {
		return err
	}
	u, err := url.Parse(endpoint.URL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || endpoint.Token == "" {
		return errors.New("invalid local Pudding MCP endpoint")
	}
	conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	u.Path = "/mcp/stdio"
	u.RawQuery = ""
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+endpoint.Token)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "pudding-stdio")
	req.Header.Set("X-Pudding-Launcher-Pid", strconv.Itoa(os.Getppid()))
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	if err = req.Write(conn); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, req)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusSwitchingProtocols || res.Header.Get("Upgrade") != "pudding-stdio" {
		return fmt.Errorf("Pudding Desktop rejected MCP connection: %s", res.Status)
	}
	if err = conn.SetDeadline(time.Time{}); err != nil {
		return err
	}
	go func() {
		_, _ = io.Copy(conn, stdin)
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	_, err = io.Copy(stdout, reader)
	return err
}
