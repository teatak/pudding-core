package mailclient

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"golang.org/x/net/html"
)

const textLimit = 64 * 1024

type Message struct {
	ID          string       `json:"id"`
	Subject     string       `json:"subject"`
	From        string       `json:"from"`
	To          string       `json:"to"`
	ReplyTo     string       `json:"replyTo,omitempty"`
	Cc          string       `json:"cc,omitempty"`
	Date        string       `json:"date"`
	Unread      bool         `json:"unread"`
	Text        string       `json:"text,omitempty"`
	Truncated   bool         `json:"truncated,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	MessageID   string       `json:"-"`
}
type Attachment struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size uint32 `json:"size"`
}
type messageID struct {
	Folder   string `json:"f"`
	Validity uint32 `json:"v"`
	UID      uint32 `json:"u"`
}
type cursor struct {
	Folder   string `json:"f"`
	Validity uint32 `json:"v"`
	Before   uint32 `json:"b"`
	Query    string `json:"q"`
}
type Page struct {
	Messages   []Message `json:"messages"`
	NextCursor string    `json:"nextCursor"`
}
type Folder struct {
	Name       string   `json:"name"`
	Attributes []string `json:"attributes"`
}

func encode(value any) string {
	b, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(b)
}
func decode(value string, target any) error {
	if len(value) > 2048 {
		return errors.New("email_invalid_id")
	}
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return errors.New("email_invalid_id")
	}
	if json.Unmarshal(b, target) != nil {
		return errors.New("email_invalid_id")
	}
	return nil
}
func validFolder(folder string) bool {
	return folder != "" && len(folder) <= 512 && !strings.ContainsAny(folder, "\x00\r\n")
}
func fetch(i *client.Client, ids *imap.SeqSet, items []imap.FetchItem) ([]*imap.Message, error) {
	ch := make(chan *imap.Message)
	done := make(chan error, 1)
	go func() { done <- i.UidFetch(ids, items, ch) }()
	var messages []*imap.Message
	for m := range ch {
		messages = append(messages, m)
	}
	if <-done != nil {
		return nil, errors.New("email_imap_failed")
	}
	return messages, nil
}
func addresses(values []*imap.Address) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value.MailboxName != "" && value.HostName != "" {
			out = append(out, (&mail.Address{Name: value.PersonalName, Address: value.MailboxName + "@" + value.HostName}).String())
		}
	}
	return strings.Join(out, ", ")
}
func summarize(m *imap.Message, folder string, validity uint32) Message {
	out := Message{ID: encode(messageID{folder, validity, m.Uid}), Unread: true}
	if e := m.Envelope; e != nil {
		h := message.Header{}
		h.Set("Subject", e.Subject)
		out.Subject, _ = h.Text("Subject")
		out.From = addresses(e.From)
		out.To = addresses(e.To)
		out.ReplyTo = addresses(e.ReplyTo)
		out.Cc = addresses(e.Cc)
		out.MessageID = e.MessageId
		if !e.Date.IsZero() {
			out.Date = e.Date.Format(time.RFC3339)
		}
	}
	if out.Date == "" && !m.InternalDate.IsZero() {
		out.Date = m.InternalDate.Format(time.RFC3339)
	}
	for _, flag := range m.Flags {
		if flag == imap.SeenFlag {
			out.Unread = false
		}
	}
	return out
}

func (c *Client) Folders(ctx context.Context) ([]Folder, error) {
	i, closeConn, err := c.imap(ctx)
	if err != nil {
		return nil, err
	}
	defer closeConn()
	ch := make(chan *imap.MailboxInfo)
	done := make(chan error, 1)
	go func() { done <- i.List("", "*", ch) }()
	out := []Folder{}
	for f := range ch {
		if len(out) >= 1000 {
			closeConn()
			continue
		}
		out = append(out, Folder{f.Name, f.Attributes})
	}
	if <-done != nil {
		return nil, errors.New("email_imap_failed")
	}
	return out, nil
}

func (c *Client) List(ctx context.Context, folder, q, token string, limit int) (Page, error) {
	out := Page{Messages: []Message{}}
	if folder == "" {
		folder = "INBOX"
	}
	if !validFolder(folder) || len(q) > 500 || limit < 1 || limit > 20 {
		return out, errors.New("email_invalid_query")
	}
	i, closeConn, err := c.imap(ctx)
	if err != nil {
		return out, err
	}
	defer closeConn()
	box, err := i.Select(folder, true)
	if err != nil {
		return out, errors.New("email_folder_unavailable")
	}
	before := box.UidNext
	qhash := fmt.Sprintf("%x", sha256.Sum256([]byte(q)))
	if token != "" {
		var cur cursor
		if err := decode(token, &cur); err != nil {
			return out, err
		}
		if cur.Folder != folder || cur.Validity != box.UidValidity || cur.Query != qhash || cur.Before < 1 {
			return out, errors.New("email_stale_cursor")
		}
		before = cur.Before
	}
	if before <= 1 {
		return out, nil
	}
	lower := uint32(1)
	if before > 5001 {
		lower = before - 5000
	}
	criteria := imap.NewSearchCriteria()
	criteria.Uid = new(imap.SeqSet)
	criteria.Uid.AddRange(lower, before-1)
	if q != "" {
		criteria.Text = []string{q}
	}
	ids, err := i.UidSearch(criteria)
	if err != nil {
		return out, errors.New("email_search_failed")
	}
	// Enforce the requested range even when a server interprets UID ranges oddly.
	filtered := ids[:0]
	for _, id := range ids {
		if id >= lower && id < before {
			filtered = append(filtered, id)
		}
	}
	ids = filtered
	sort.Slice(ids, func(a, b int) bool { return ids[a] > ids[b] })
	next := lower
	if len(ids) > limit {
		ids = ids[:limit]
		next = ids[len(ids)-1]
	}
	if next > 1 {
		out.NextCursor = encode(cursor{folder, box.UidValidity, next, qhash})
	}
	if len(ids) == 0 {
		return out, nil
	}
	set := new(imap.SeqSet)
	set.AddNum(ids...)
	messages, err := fetch(i, set, []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchFlags, imap.FetchInternalDate})
	if err != nil {
		return out, err
	}
	sort.Slice(messages, func(a, b int) bool { return messages[a].Uid > messages[b].Uid })
	for _, m := range messages {
		out.Messages = append(out.Messages, summarize(m, folder, box.UidValidity))
	}
	return out, nil
}

func (c *Client) Read(ctx context.Context, id string, metadataOnly bool) (Message, error) {
	var out Message
	var key messageID
	if err := decode(id, &key); err != nil {
		return out, err
	}
	if !validFolder(key.Folder) || key.UID == 0 || key.Validity == 0 {
		return out, errors.New("email_invalid_id")
	}
	i, closeConn, err := c.imap(ctx)
	if err != nil {
		return out, err
	}
	defer closeConn()
	box, err := i.Select(key.Folder, true)
	if err != nil {
		return out, errors.New("email_folder_unavailable")
	}
	if box.UidValidity != key.Validity {
		return out, errors.New("email_stale_message")
	}
	set := new(imap.SeqSet)
	set.AddNum(key.UID)
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, imap.FetchFlags, imap.FetchInternalDate}
	if !metadataOnly {
		items = append(items, imap.FetchBodyStructure)
	}
	messages, err := fetch(i, set, items)
	if err != nil {
		return out, err
	}
	if len(messages) != 1 {
		return out, errors.New("email_message_not_found")
	}
	m := messages[0]
	out = summarize(m, key.Folder, key.Validity)
	if metadataOnly || m.BodyStructure == nil {
		return out, nil
	}
	type textPart struct {
		path []int
		body *imap.BodyStructure
	}
	var plain, htmlParts []textPart
	count := 0
	m.BodyStructure.Walk(func(path []int, p *imap.BodyStructure) bool {
		count++
		if count > 128 {
			out.Truncated = true
			return false
		}
		if strings.EqualFold(p.Disposition, "attachment") || p.DispositionParams["filename"] != "" {
			name := p.DispositionParams["filename"]
			if name == "" {
				name = p.Params["name"]
			}
			out.Attachments = append(out.Attachments, Attachment{name, p.MIMEType + "/" + p.MIMESubType, p.Size})
			return false
		}
		if strings.EqualFold(p.MIMEType, "text") {
			part := textPart{append([]int(nil), path...), p}
			switch strings.ToLower(p.MIMESubType) {
			case "plain":
				plain = append(plain, part)
			case "html":
				htmlParts = append(htmlParts, part)
			}
		}
		return !strings.EqualFold(p.MIMEType, "message")
	})
	parts := plain
	if len(parts) == 0 {
		parts = htmlParts
	}
	if len(parts) > 16 {
		parts = parts[:16]
		out.Truncated = true
	}
	remaining := textLimit
	for _, part := range parts {
		if remaining <= 0 {
			out.Truncated = true
			break
		}
		section := &imap.BodySectionName{BodyPartName: imap.BodyPartName{Path: part.path}, Peek: true, Partial: []int{0, remaining}}
		values, err := fetch(i, set, []imap.FetchItem{section.FetchItem()})
		if err != nil {
			return out, err
		}
		if len(values) != 1 || values[0].GetBody(section) == nil {
			return out, errors.New("email_body_unavailable")
		}
		raw, err := io.ReadAll(io.LimitReader(values[0].GetBody(section), int64(remaining)))
		if err != nil {
			return out, errors.New("email_decode_failed")
		}
		if int(part.body.Size) > len(raw) {
			out.Truncated = true
		}
		contentType := mime.FormatMediaType(part.body.MIMEType+"/"+part.body.MIMESubType, part.body.Params)
		h := message.Header{}
		h.Set("Content-Type", contentType)
		h.Set("Content-Transfer-Encoding", part.body.Encoding)
		entity, err := message.New(h, strings.NewReader(string(raw)))
		if err != nil {
			return out, errors.New("email_decode_failed")
		}
		decoded, err := io.ReadAll(io.LimitReader(entity.Body, int64(remaining)))
		if err != nil && !out.Truncated {
			return out, errors.New("email_decode_failed")
		}
		text := strings.ToValidUTF8(string(decoded), "\uFFFD")
		if strings.EqualFold(part.body.MIMESubType, "html") {
			text = plainHTML(text)
		}
		if out.Text != "" {
			out.Text += "\n"
		}
		out.Text += text
		if len(out.Text) >= textLimit {
			out.Text = out.Text[:textLimit]
			for !utf8.ValidString(out.Text) {
				out.Text = out.Text[:len(out.Text)-1]
			}
			out.Truncated = true
		}
		remaining = textLimit - len(out.Text) - 1
	}
	return out, nil
}

// Return inert text only. Remote images, scripts, styles and tracking pixels are
// never rendered or requested by the Email board.
func plainHTML(value string) string {
	z := html.NewTokenizer(strings.NewReader(value))
	var out strings.Builder
	hidden := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.TrimSpace(out.String())
		case html.StartTagToken:
			t := z.Token()
			if t.Data == "script" || t.Data == "style" || t.Data == "head" {
				hidden++
			}
			if hidden == 0 && (t.Data == "br" || t.Data == "p" || t.Data == "div" || t.Data == "li") {
				out.WriteByte('\n')
			}
		case html.EndTagToken:
			t := z.Token()
			if hidden > 0 && (t.Data == "script" || t.Data == "style" || t.Data == "head") {
				hidden--
			}
		case html.TextToken:
			if hidden == 0 {
				out.Write(z.Text())
			}
		}
	}
}
