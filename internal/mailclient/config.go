// Package mailclient implements the native Email connector. Credentials never
// cross the tool/widget boundary; only the host supplies Config.
package mailclient

import (
	"crypto/sha256"
	"fmt"
	"net"
	"net/mail"
	"strconv"
	"strings"
)

type Server struct {
	Host                         string
	Port                         int
	Security, Username, Password string
}
type Config struct {
	Address, MailboxID string
	IMAP, SMTP         Server
}

func Parse(method, address, password string, fields map[string]string) (Config, error) {
	var c Config
	address = strings.TrimSpace(address)
	a, err := mail.ParseAddress(address)
	if err != nil || a.Address != address || strings.ContainsAny(address, "\r\n\x00") || password == "" {
		return c, fmt.Errorf("email_invalid_credentials")
	}
	c.Address = address
	switch method {
	case "custom":
		for _, item := range []struct {
			prefix string
			server *Server
		}{{"imap", &c.IMAP}, {"smtp", &c.SMTP}} {
			s := item.server
			s.Host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(fields[item.prefix+"Host"]), "."))
			s.Port, err = strconv.Atoi(fields[item.prefix+"Port"])
			s.Security = fields[item.prefix+"Security"]
			if err != nil || s.Port < 1 || s.Port > 65535 || s.Host == "" || strings.ContainsAny(s.Host, "/@ \r\n\x00[]") || (strings.Contains(s.Host, ":") && net.ParseIP(s.Host) == nil) || (s.Security != "tls" && s.Security != "starttls") {
				return c, fmt.Errorf("email_invalid_server")
			}
		}
	default:
		preset, ok := emailServerPresets[method]
		if !ok {
			return c, fmt.Errorf("email_invalid_provider")
		}
		c.IMAP = Server{Host: preset.imap, Port: 993, Security: "tls"}
		c.SMTP = Server{Host: preset.smtp, Port: preset.smtpPort, Security: preset.smtpSecurity}
	}
	c.IMAP.Username, c.SMTP.Username = address, address
	if value := strings.TrimSpace(fields["imapUsername"]); value != "" {
		c.IMAP.Username = value
	}
	if value := strings.TrimSpace(fields["smtpUsername"]); value != "" {
		c.SMTP.Username = value
	}
	if method != "custom" {
		c.IMAP.Username = strings.ToLower(c.IMAP.Username)
		c.SMTP.Username = strings.ToLower(c.SMTP.Username)
	}
	for _, username := range []string{c.IMAP.Username, c.SMTP.Username} {
		if strings.ContainsAny(username, "\r\n\x00") {
			return c, fmt.Errorf("email_invalid_credentials")
		}
	}
	c.IMAP.Password, c.SMTP.Password = password, password
	if value := fields["smtpPassword"]; value != "" {
		c.SMTP.Password = value
	}
	c.MailboxID = fmt.Sprintf("mb_%x", sha256.Sum256([]byte(net.JoinHostPort(c.IMAP.Host, strconv.Itoa(c.IMAP.Port))+"\x00"+c.IMAP.Username)))
	return c, nil
}

// Presets are host-owned; request fields cannot redirect their servers.
var emailServerPresets = map[string]struct {
	imap, smtp   string
	smtpPort     int
	smtpSecurity string
}{
	"icloud":   {"imap.mail.me.com", "smtp.mail.me.com", 587, "starttls"},
	"gmail":    {"imap.gmail.com", "smtp.gmail.com", 465, "tls"},
	"qq":       {"imap.qq.com", "smtp.qq.com", 465, "tls"},
	"163":      {"imap.163.com", "smtp.163.com", 465, "tls"},
	"126":      {"imap.126.com", "smtp.126.com", 465, "tls"},
	"yahoo":    {"imap.mail.yahoo.com", "smtp.mail.yahoo.com", 465, "tls"},
	"fastmail": {"imap.fastmail.com", "smtp.fastmail.com", 465, "tls"},
}
