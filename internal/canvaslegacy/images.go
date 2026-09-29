package canvaslegacy

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teatak/pudding-core/contracts"
)

// Images embeds images into the source package. Remote reads are bounded,
// unauthenticated and restricted to public addresses (including redirects).
func Images(home string) func(string) (string, error) {
	cache := map[string]string{}
	return func(ref string) (string, error) {
		if cached, ok := cache[ref]; ok {
			return cached, nil
		}
		if strings.HasPrefix(ref, "data:image/") {
			return ref, nil
		}
		u, err := url.Parse(ref)
		if err != nil {
			return "", err
		}
		var reader io.ReadCloser
		var contentType string
		switch u.Scheme {
		case "https", "http":
			if u.User != nil {
				return "", errors.New("image URL must not contain credentials")
			}
			transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				if len(ips) == 0 {
					return nil, errors.New("image host has no addresses")
				}
				for _, addr := range ips {
					ip := addr.IP
					if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
						return nil, errors.New("remote image must use a public address")
					}
				}
				return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
			}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 || req.URL.User != nil || (req.URL.Scheme != "https" && req.URL.Scheme != "http") {
					return errors.New("invalid image redirect")
				}
				return nil
			}}
			resp, err := client.Get(ref)
			if err != nil {
				return "", fmt.Errorf("read canvas image: %w", err)
			}
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return "", fmt.Errorf("canvas image returned HTTP %d", resp.StatusCode)
			}
			reader = resp.Body
			contentType = resp.Header.Get("Content-Type")
		case "file", "":
			local := u.Path
			if strings.HasPrefix(local, "/sessions/") {
				parts := strings.SplitN(strings.TrimPrefix(local, "/sessions/"), "/attachments/", 2)
				if len(parts) != 2 || strings.Contains(parts[0], "/") || parts[0] == ".." {
					return "", errors.New("invalid attachment image path")
				}
				root, err := os.OpenRoot(filepath.Join(home, "attachments", "sessions"))
				if err != nil {
					return "", err
				}
				defer root.Close()
				reader, err = root.Open(filepath.Join(parts[0], parts[1]))
				if err != nil {
					return "", err
				}
			} else {
				if u.Scheme != "file" || u.Host != "" || !filepath.IsAbs(local) {
					return "", errors.New("unsupported canvas image path")
				}
				stat, err := os.Lstat(local)
				if err != nil {
					return "", err
				}
				if !stat.Mode().IsRegular() {
					return "", errors.New("canvas image must be a regular file")
				}
				reader, err = os.Open(local)
				if err != nil {
					return "", err
				}
			}
			contentType = mime.TypeByExtension(filepath.Ext(local))
		default:
			return "", fmt.Errorf("unsupported image scheme %q", u.Scheme)
		}
		defer reader.Close()
		limit := contracts.Canvas().MaxFileBytes*3/4 - 1024
		data, err := io.ReadAll(io.LimitReader(reader, int64(limit+1)))
		if err != nil {
			return "", err
		}
		if len(data) > limit {
			return "", errors.New("canvas image exceeds source file limit")
		}
		if contentType == "" {
			contentType = http.DetectContentType(data)
		}
		contentType, _, err = mime.ParseMediaType(contentType)
		if err != nil || !strings.HasPrefix(contentType, "image/") {
			return "", errors.New("canvas image response is not an image")
		}
		result := "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
		cache[ref] = result
		return result, nil
	}
}
