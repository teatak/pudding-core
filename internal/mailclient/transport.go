package mailclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	imapid "github.com/emersion/go-imap-id"
	"github.com/emersion/go-imap/client"
)

// RootCAs is only needed by isolated TLS fixtures. Production uses system roots.
type Client struct {
	Config  Config
	RootCAs *x509.CertPool
}

func (c *Client) dial(ctx context.Context, server Server) (net.Conn, *tls.Config, func(), error) {
	tlsConfig := &tls.Config{ServerName: server.Host, MinVersion: tls.VersionTLS12, RootCAs: c.RootCAs}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(server.Host, strconv.Itoa(server.Port)))
	if err != nil {
		return nil, nil, nil, errors.New("email_connection_failed")
	}
	deadline := time.Now().Add(30 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	closeConn := func() { stop(); _ = conn.Close() }
	if server.Security == "tls" {
		secured := tls.Client(conn, tlsConfig)
		if err := secured.HandshakeContext(ctx); err != nil {
			closeConn()
			return nil, nil, nil, errors.New("email_tls_failed")
		}
		return secured, tlsConfig, closeConn, nil
	}
	return conn, tlsConfig, closeConn, nil
}

func (c *Client) imap(ctx context.Context) (*client.Client, func(), error) {
	conn, tc, closeConn, err := c.dial(ctx, c.Config.IMAP)
	if err != nil {
		return nil, nil, err
	}
	i, err := client.New(conn)
	if err != nil {
		closeConn()
		return nil, nil, errors.New("email_imap_failed")
	}
	i.Timeout = 30 * time.Second
	if c.Config.IMAP.Security == "starttls" {
		if err := i.StartTLS(tc); err != nil {
			closeConn()
			return nil, nil, errors.New("email_tls_failed")
		}
	}
	identity := imapid.NewClient(i)
	supported, err := identity.SupportID()
	if err != nil {
		closeConn()
		return nil, nil, errors.New("email_imap_failed")
	}
	if supported {
		if _, err := identity.ID(imapid.ID{imapid.FieldName: "Pudding", imapid.FieldVersion: "1.0"}); err != nil {
			closeConn()
			return nil, nil, errors.New("email_imap_failed")
		}
	}
	if err := i.Login(c.Config.IMAP.Username, c.Config.IMAP.Password); err != nil {
		closeConn()
		return nil, nil, errors.New("email_imap_auth_failed")
	}
	return i, closeConn, nil
}

func (c *Client) smtp(ctx context.Context) (*smtp.Client, func(), error) {
	conn, tc, closeConn, err := c.dial(ctx, c.Config.SMTP)
	if err != nil {
		return nil, nil, err
	}
	s, err := smtp.NewClient(conn, c.Config.SMTP.Host)
	if err != nil {
		closeConn()
		return nil, nil, errors.New("email_smtp_failed")
	}
	if c.Config.SMTP.Security == "starttls" {
		if err := s.StartTLS(tc); err != nil {
			closeConn()
			return nil, nil, errors.New("email_tls_failed")
		}
	}
	var auth smtp.Auth = smtp.PlainAuth("", c.Config.SMTP.Username, c.Config.SMTP.Password, c.Config.SMTP.Host)
	if _, methods := s.Extension("AUTH"); !strings.Contains(strings.ToUpper(methods), "PLAIN") && strings.Contains(strings.ToUpper(methods), "LOGIN") {
		auth = &loginAuth{username: c.Config.SMTP.Username, password: c.Config.SMTP.Password}
	}
	if err := s.Auth(auth); err != nil {
		closeConn()
		return nil, nil, errors.New("email_smtp_auth_failed")
	}
	return s, closeConn, nil
}

type loginAuth struct {
	username, password string
	step               int
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS {
		return "", nil, errors.New("email_tls_required")
	}
	return "LOGIN", nil, nil
}
func (a *loginAuth) Next(_ []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	a.step++
	switch a.step {
	case 1:
		return []byte(a.username), nil
	case 2:
		return []byte(a.password), nil
	default:
		return nil, errors.New("email_smtp_auth_failed")
	}
}

func (c *Client) Validate(ctx context.Context) error {
	_, closeIMAP, err := c.imap(ctx)
	if err != nil {
		return err
	}
	closeIMAP()
	_, closeSMTP, err := c.smtp(ctx)
	if err != nil {
		return err
	}
	closeSMTP()
	return nil
}
