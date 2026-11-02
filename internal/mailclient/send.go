package mailclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/mail"
	"strings"
	"time"

	messagemail "github.com/emersion/go-message/mail"
)

type SendRequest struct {
	To        []string `json:"to"`
	Cc        []string `json:"cc,omitempty"`
	Bcc       []string `json:"bcc,omitempty"`
	Subject   string   `json:"subject"`
	Text      string   `json:"text"`
	ReplyToID string   `json:"replyToID,omitempty"`
}

// Send is deliberately single-attempt. Once DATA begins, transport failures may
// mean the server accepted the message; callers must not automatically retry.
func (c *Client) Send(ctx context.Context, request SendRequest) (map[string]any, error) {
	if len(request.To) == 0 || len(request.To)+len(request.Cc)+len(request.Bcc) > 100 || len(request.Subject) > 1000 || strings.ContainsAny(request.Subject, "\r\n\x00") || len(request.Text) > 256*1024 || strings.TrimSpace(request.Text) == "" {
		return nil, errors.New("email_invalid_message")
	}
	header := messagemail.Header{}
	header.SetAddressList("From", []*mail.Address{{Address: c.Config.Address}})
	var recipients []string
	for _, group := range []struct {
		name   string
		values []string
	}{{"To", request.To}, {"Cc", request.Cc}, {"Bcc", request.Bcc}} {
		var addrs []*mail.Address
		for _, value := range group.values {
			a, err := mail.ParseAddress(value)
			if err != nil || strings.ContainsAny(value, "\r\n\x00") {
				return nil, errors.New("email_invalid_recipient")
			}
			addrs = append(addrs, a)
			recipients = append(recipients, a.Address)
		}
		if group.name != "Bcc" && len(addrs) > 0 {
			header.SetAddressList(group.name, addrs)
		}
	}
	if request.ReplyToID != "" {
		original, err := c.Read(ctx, request.ReplyToID, true)
		if err != nil {
			return nil, err
		}
		if original.MessageID != "" {
			if strings.ContainsAny(original.MessageID, "\r\n\x00") {
				return nil, errors.New("email_invalid_reply")
			}
			header.SetMsgIDList("In-Reply-To", []string{strings.Trim(original.MessageID, "<>")})
		}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, errors.New("email_message_id_failed")
	}
	domain := c.Config.Address[strings.LastIndex(c.Config.Address, "@")+1:]
	id := hex.EncodeToString(nonce[:]) + "@" + domain
	header.SetMessageID(id)
	header.SetSubject(request.Subject)
	header.SetDate(time.Now())
	header.Set("Content-Type", "text/plain; charset=utf-8")
	var body bytes.Buffer
	w, err := messagemail.CreateSingleInlineWriter(&body, header)
	if err != nil {
		return nil, errors.New("email_encode_failed")
	}
	if _, err = w.Write([]byte(request.Text)); err != nil {
		return nil, errors.New("email_encode_failed")
	}
	if err = w.Close(); err != nil {
		return nil, errors.New("email_encode_failed")
	}
	s, closeConn, err := c.smtp(ctx)
	if err != nil {
		return nil, err
	}
	defer closeConn()
	if err := s.Mail(c.Config.Address); err != nil {
		return nil, errors.New("email_sender_rejected")
	}
	for _, recipient := range recipients {
		if err := s.Rcpt(recipient); err != nil {
			return nil, errors.New("email_recipient_rejected")
		}
	}
	w, err = s.Data()
	if err != nil {
		return nil, errors.New("email_data_rejected")
	}
	result := map[string]any{"messageID": id, "sentCopy": "server_managed"}
	if _, err = io.Copy(w, &body); err != nil {
		return result, errors.New("email_send_uncertain")
	}
	if err = w.Close(); err != nil {
		return result, errors.New("email_send_uncertain")
	}
	// QUIT is cleanup, and cannot change an accepted delivery into a failure.
	_ = s.Quit()
	result["accepted"] = true
	return result, nil
}
