package pluginexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/teatak/pudding-core/internal/mailclient"
	"github.com/teatak/pudding-core/internal/plugin"
)

func emailRequest(ctx context.Context, binding *plugin.EndpointBinding, args map[string]any, limit int) map[string]any {
	result := func(body any, err error) map[string]any {
		if err != nil {
			return map[string]any{"ok": false, "status": 400, "reason": err.Error(), "error": err.Error(), "body_json": body}
		}
		encoded, err := json.Marshal(body)
		if err != nil || len(encoded) > limit {
			return map[string]any{"ok": false, "status": 413, "reason": "response_too_large"}
		}
		// Match HTTP responses: JSON objects must be maps for responsePath and
		// tool summaries, even when the native client returns a typed struct.
		var value any
		_ = json.Unmarshal(encoded, &value)
		return map[string]any{"ok": true, "status": 200, "body_json": value}
	}
	cfg, err := mailclient.Parse(binding.Auth.MethodID, binding.Auth.Username, binding.Auth.Password, binding.ConnectionFields)
	if err != nil {
		return result(nil, err)
	}
	client := &mailclient.Client{Config: cfg}
	path := stringArg(args, "path")
	method := strings.ToUpper(stringArg(args, "method"))
	if method == "" {
		method = "GET"
	}
	query := map[string]any{}
	if value := args["query"]; value != nil {
		b, err := json.Marshal(value)
		if err != nil || json.Unmarshal(b, &query) != nil {
			return result(nil, errors.New("email_invalid_query"))
		}
	}
	q := func(key string) string {
		if value, ok := query[key]; ok {
			return fmt.Sprint(value)
		}
		return ""
	}
	if method == "GET" && path == "/profile" {
		return result(map[string]any{"mailboxID": cfg.MailboxID, "address": cfg.Address, "configRevision": binding.ConfigRevision}, nil)
	}
	if q("mailboxID") != cfg.MailboxID || q("configRevision") != binding.ConfigRevision {
		return result(nil, errors.New("email_connection_changed"))
	}
	switch {
	case method == "GET" && path == "/folders":
		folders, err := client.Folders(ctx)
		return result(map[string]any{"folders": folders}, err)
	case method == "GET" && path == "/messages":
		count := 20
		if q("limit") != "" {
			count, err = strconv.Atoi(q("limit"))
			if err != nil {
				return result(nil, errors.New("email_invalid_query"))
			}
		}
		page, err := client.List(ctx, q("folder"), q("q"), q("cursor"), count)
		return result(page, err)
	case method == "GET" && strings.HasPrefix(path, "/messages/"):
		message, err := client.Read(ctx, strings.TrimPrefix(path, "/messages/"), q("metadataOnly") == "true")
		return result(message, err)
	case method == "POST" && path == "/send":
		b, err := json.Marshal(args["body_json"])
		if err != nil || len(b) > endpointMaxRequestBytes {
			return result(nil, errors.New("email_invalid_message"))
		}
		var request mailclient.SendRequest
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil {
			return result(nil, errors.New("email_invalid_message"))
		}
		response, err := client.Send(ctx, request)
		return result(response, err)
	default:
		return result(nil, errors.New("email_unsupported_operation"))
	}
}
