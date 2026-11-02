package mailclient

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	imapid "github.com/emersion/go-imap-id"
	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"
)

func TestConfigurationIdentity(t *testing.T) {
	a, err := Parse("gmail", "User@gmail.com", "one", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Parse("gmail", "user@gmail.com", "two", map[string]string{"imapHost": "attacker.test"})
	if a.MailboxID != b.MailboxID || b.IMAP.Host != "imap.gmail.com" {
		t.Fatal("rotation or preset identity changed")
	}
	c, _ := Parse("icloud", "user@icloud.com", "app-password", nil)
	if c.SMTP.Security != "starttls" || c.SMTP.Port != 587 || c.MailboxID == a.MailboxID {
		t.Fatal(c)
	}
	for _, fields := range []map[string]string{{}, {"imapHost": "imap.test", "imapPort": "143", "imapSecurity": "none"}} {
		if _, err := Parse("custom", "user@test.dev", "password", fields); err == nil {
			t.Fatal("accepted incomplete or plaintext server")
		}
	}
}

func TestEmailPresetsPinServersAndDefaultLogins(t *testing.T) {
	for _, test := range []struct{ method, imap, smtp string }{{"qq", "imap.qq.com", "smtp.qq.com"}, {"163", "imap.163.com", "smtp.163.com"}, {"126", "imap.126.com", "smtp.126.com"}, {"yahoo", "imap.mail.yahoo.com", "smtp.mail.yahoo.com"}, {"fastmail", "imap.fastmail.com", "smtp.fastmail.com"}} {
		t.Run(test.method, func(t *testing.T) {
			cfg, err := Parse(test.method, "User@example.com", "app-secret", map[string]string{"imapHost": "attacker.test", "smtpHost": "attacker.test"})
			if err != nil {
				t.Fatal(err)
			}
			if cfg.IMAP.Host != test.imap || cfg.SMTP.Host != test.smtp || cfg.IMAP.Port != 993 || cfg.SMTP.Port != 465 || cfg.IMAP.Security != "tls" || cfg.SMTP.Security != "tls" {
				t.Fatal(cfg)
			}
			if cfg.IMAP.Username != "user@example.com" || cfg.SMTP.Username != cfg.IMAP.Username || cfg.IMAP.Password != "app-secret" || cfg.SMTP.Password != cfg.IMAP.Password {
				t.Fatal("preset defaults differ")
			}
			rotated, _ := Parse(test.method, "user@example.com", "replacement", nil)
			if cfg.MailboxID != rotated.MailboxID {
				t.Fatal("rotation changed mailbox")
			}
		})
	}
}

type observedIDExtension struct {
	server.Extension
	seen *atomic.Bool
}

func (e observedIDExtension) Command(name string) server.HandlerFactory {
	factory := e.Extension.Command(name)
	if factory == nil {
		return nil
	}
	return func() server.Handler { return &observedIDHandler{Handler: factory(), seen: e.seen} }
}

type observedIDHandler struct {
	server.Handler
	seen *atomic.Bool
}

func (h *observedIDHandler) Handle(conn server.Conn) error {
	if id, ok := h.Handler.(*imapid.Handler); ok && id.ID[imapid.FieldName] == "Pudding" {
		h.seen.Store(true)
	}
	return h.Handler.Handle(conn)
}

func tlsFixture(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	h := httptest.NewTLSServer(nil)
	defer h.Close()
	roots := x509.NewCertPool()
	roots.AddCert(h.Certificate())
	return h.TLS.Clone(), roots
}

func TestIMAPReadSearchAndIdentity(t *testing.T) {
	tc, roots := tlsFixture(t)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", tc)
	if err != nil {
		t.Fatal(err)
	}
	backend := memory.New()
	u, _ := backend.Login(nil, "username", "password")
	box, _ := u.GetMailbox("INBOX")
	mb := box.(*memory.Mailbox)
	mb.Messages[0].Flags = nil
	body := "From: Sender <sender@example.org>\r\nTo: user@example.org\r\nSubject: =?utf-8?b?5L2g5aW9?=\r\nMessage-ID: <original@example.org>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Hello &amp; welcome</p><script>hidden()</script><img src=\"https://tracker.test\">\r\n--b\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=large.bin\r\n\r\n" + strings.Repeat("x", textLimit*3) + "\r\n--b--\r\n"
	mb.Messages[0].Body = []byte(body)
	mb.Messages[0].Size = uint32(len(body))
	s := server.New(backend)
	var identified atomic.Bool
	s.Enable(observedIDExtension{Extension: imapid.NewExtension(imapid.ID{imapid.FieldName: "Fixture"}), seen: &identified})
	s.TLSConfig = tc
	s.ErrorLog = discardLogger{}
	go s.Serve(listener)
	t.Cleanup(func() { _ = s.Close(); _ = listener.Close() })
	c := &Client{RootCAs: roots, Config: Config{IMAP: Server{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Security: "tls", Username: "username", Password: "password"}}}
	page, err := c.List(context.Background(), "INBOX", "", "", 20)
	if err != nil || len(page.Messages) != 1 {
		t.Fatalf("list: %+v %v", page, err)
	}
	if !identified.Load() {
		t.Fatal("advertised IMAP ID was not sent")
	}
	if page.Messages[0].Subject != "你好" || !page.Messages[0].Unread || page.Messages[0].Text != "" {
		t.Fatal(page.Messages)
	}
	m, err := c.Read(context.Background(), page.Messages[0].ID, false)
	if err != nil || !strings.Contains(m.Text, "Hello & welcome") || strings.Contains(m.Text, "hidden") || strings.Contains(m.Text, "tracker") || len(m.Attachments) != 1 || len(mb.Messages[0].Flags) != 0 {
		t.Fatalf("read: %+v %v", m, err)
	}
	missing, err := c.List(context.Background(), "INBOX", "no-such-subject", "", 20)
	if err != nil || len(missing.Messages) != 0 {
		t.Fatal(missing, err)
	}
	var id messageID
	_ = decode(m.ID, &id)
	id.Validity++
	if _, err = c.Read(context.Background(), encode(id), false); err == nil || err.Error() != "email_stale_message" {
		t.Fatal("UIDVALIDITY not enforced", err)
	}
	// UID gaps must page forward without repeating or dropping older messages.
	template := *mb.Messages[0]
	mb.Messages = nil
	for n := uint32(1); n <= 25; n++ {
		m := template
		m.Uid = n
		mb.Messages = append(mb.Messages, &m)
	}
	first, err := c.List(context.Background(), "INBOX", "", "", 20)
	if err != nil || len(first.Messages) != 20 || first.NextCursor == "" {
		t.Fatal("first page", first, err)
	}
	second, err := c.List(context.Background(), "INBOX", "", first.NextCursor, 20)
	if err != nil || len(second.Messages) != 5 || second.NextCursor != "" {
		t.Fatal("second page", second, err)
	}
	seen := map[string]bool{}
	for _, m := range append(first.Messages, second.Messages...) {
		if seen[m.ID] {
			t.Fatal("duplicate UID")
		}
		seen[m.ID] = true
	}
	if _, err := c.List(context.Background(), "INBOX", "changed search", first.NextCursor, 20); err == nil || err.Error() != "email_stale_cursor" {
		t.Fatal("cursor reused with another search", err)
	}
	mb.Messages = mb.Messages[:2]
	mb.Messages[1].Uid = 12000
	window, err := c.List(context.Background(), "INBOX", "no-such-subject", "", 20)
	if err != nil || len(window.Messages) != 0 || window.NextCursor == "" {
		t.Fatal("empty search window lost continuation", window, err)
	}
	c.Config.IMAP.Password = "private-password"
	if _, err = c.List(context.Background(), "INBOX", "", "", 20); err == nil || err.Error() != "email_imap_auth_failed" {
		t.Fatal("unsafe auth error", err)
	}
	c.RootCAs = nil
	if _, err = c.List(context.Background(), "INBOX", "", "", 20); err == nil || err.Error() != "email_tls_failed" {
		t.Fatal("untrusted TLS accepted", err)
	}
}

type discardLogger struct{}

func (discardLogger) Println(...interface{}) {}

func (discardLogger) Printf(string, ...interface{}) {}

func TestSMTPTransportAndUncertainDelivery(t *testing.T) {
	for _, mode := range []string{"tls", "starttls", "login", "uncertain"} {
		t.Run(mode, func(t *testing.T) {
			tc, roots := tlsFixture(t)
			var listener net.Listener
			var err error
			if mode == "starttls" {
				listener, err = net.Listen("tcp", "127.0.0.1:0")
			} else {
				listener, err = tls.Listen("tcp", "127.0.0.1:0", tc)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			captured := make(chan string, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				r := textproto.NewReader(bufio.NewReader(conn))
				write := func(value string) { _, _ = io.WriteString(conn, value+"\r\n") }
				write("220 fixture ESMTP")
				for {
					line, err := r.ReadLine()
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						if mode == "login" {
							write("250-fixture\r\n250 AUTH LOGIN")
						} else {
							write("250-fixture\r\n250-STARTTLS\r\n250 AUTH PLAIN")
						}
					case line == "STARTTLS":
						write("220 ready")
						conn = tls.Server(conn, tc)
						r = textproto.NewReader(bufio.NewReader(conn))
					case line == "AUTH LOGIN":
						write("334 VXNlcm5hbWU6")
						user, _ := r.ReadLine()
						write("334 UGFzc3dvcmQ6")
						pass, _ := r.ReadLine()
						if user != base64.StdEncoding.EncodeToString([]byte("user@example.org")) || pass != base64.StdEncoding.EncodeToString([]byte("app-secret")) {
							write("535 wrong")
							return
						}
						write("235 authenticated")
					case strings.HasPrefix(line, "AUTH PLAIN "):
						decoded, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "AUTH PLAIN "))
						if string(decoded) != "\x00user@example.org\x00app-secret" {
							write("535 wrong")
							return
						}
						write("235 authenticated")
					case strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
						write("250 ok")
					case line == "DATA":
						write("354 continue")
						b, err := io.ReadAll(r.DotReader())
						if err != nil {
							return
						}
						captured <- string(b)
						if mode == "uncertain" {
							return
						}
						write("250 accepted")
					case line == "QUIT":
						write("221 bye")
						return
					default:
						write("500 unexpected")
					}
				}
			}()
			security := mode
			if security == "uncertain" || security == "login" {
				security = "tls"
			}
			c := &Client{RootCAs: roots, Config: Config{Address: "user@example.org", SMTP: Server{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Security: security, Username: "user@example.org", Password: "app-secret"}}}
			result, err := c.Send(context.Background(), SendRequest{To: []string{"to@example.org"}, Bcc: []string{"hidden@example.org"}, Subject: "你好", Text: "Hello 世界"})
			if mode == "uncertain" {
				if err == nil || err.Error() != "email_send_uncertain" || result["messageID"] == nil {
					t.Fatal(result, err)
				}
			} else if err != nil || result["accepted"] != true {
				t.Fatal(result, err)
			}
			<-finished
			select {
			case body := <-captured:
				if strings.Contains(body, "Bcc:") || strings.Contains(body, "app-secret") || !strings.Contains(body, "From: <user@example.org>") {
					t.Fatal("invalid MIME", body)
				}
			default:
				t.Fatal("no message captured")
			}
		})
	}
}

func TestSendRejectsHeaderInjectionBeforeNetwork(t *testing.T) {
	c := &Client{Config: Config{Address: "sender@example.org"}}
	for _, r := range []SendRequest{{To: []string{"a@example.org"}, Subject: "hello\r\nBcc: evil@example.org", Text: "test"}, {To: []string{"a@example.org\r\n"}, Text: "test"}} {
		_, err := c.Send(context.Background(), r)
		if err == nil || !strings.HasPrefix(err.Error(), "email_invalid_") {
			t.Fatal(fmt.Sprint(err))
		}
	}
}
