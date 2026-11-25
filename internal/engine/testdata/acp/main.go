// A self-contained ACP peer used to exercise the real process transport.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	enc := json.NewEncoder(os.Stdout)
	reply := func(id any, result any) { _ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result}) }
	notify := func(id string, update any) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": id, "update": update}})
	}
	log, _ := os.OpenFile("rpc.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	defer log.Close()
	sessionID, model, effort, hostName := "", "", "", ""
	configOptions := func() []any {
		levels := []map[string]string{{"value": "low", "name": "Low"}, {"value": "high", "name": "High"}}
		if model == "pro" {
			levels = append(levels, map[string]string{"value": "max", "name": "Maximum"})
		}
		return []any{
			map[string]any{"id": "model", "type": "select", "currentValue": model, "options": []map[string]string{{"value": "flash", "name": "Flash"}, {"value": "pro", "name": "Pro"}}},
			map[string]any{"id": "thought_level", "type": "select", "currentValue": effort, "options": levels},
		}
	}
	var pending any
	failedToolOnCancel := false
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var m struct {
			ID     any
			Method string
			Params json.RawMessage
			Result json.RawMessage
		}
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			os.Exit(2)
		}
		fmt.Fprintln(log, string(scanner.Bytes()))
		var p struct {
			SessionID  string `json:"sessionId"`
			ConfigID   string `json:"configId"`
			Value      string
			Prompt     []struct{ Type, Text string }
			MCPServers []struct{ Name string } `json:"mcpServers"`
		}
		_ = json.Unmarshal(m.Params, &p)
		switch m.Method {
		case "initialize":
			cap := map[string]any{"loadSession": true, "promptCapabilities": map[string]bool{"image": true, "embeddedContext": true}, "sessionCapabilities": map[string]any{"resume": map[string]any{}, "close": map[string]any{}}}
			if _, err := os.Stat("load-only"); err == nil {
				delete(cap["sessionCapabilities"].(map[string]any), "resume")
			}
			reply(m.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": cap})
		case "session/new", "session/resume", "session/load":
			hostName = ""
			if len(p.MCPServers) > 0 {
				hostName = p.MCPServers[0].Name
			}
			if m.Method != "session/new" {
				if _, err := os.Stat("restore-fails"); err == nil {
					_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32603, "message": "session not found"}})
					continue
				}
			}
			sessionID = p.SessionID
			if sessionID == "" {
				sessionID = fmt.Sprint("agent-", time.Now().UnixNano())
			}
			if m.Method == "session/load" {
				notify(sessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "OLD REPLAY"}})
			}
			model, effort = "flash", "high"
			reply(m.ID, map[string]any{"sessionId": sessionID, "configOptions": configOptions()})
		case "session/set_config_option":
			if p.Value == "invalid" {
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32602, "message": "unsupported option: invalid"}})
				continue
			}
			if p.ConfigID == "model" {
				model = p.Value
			}
			if p.ConfigID == "thought_level" {
				effort = p.Value
			}
			reply(m.ID, map[string]any{"configOptions": configOptions()})
		case "":
			if pending != nil && len(m.Result) > 0 {
				notify(sessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": string(m.Result)}})
				reply(pending, map[string]string{"stopReason": "end_turn"})
				pending = nil
			}
		case "session/prompt":
			text := ""
			for _, part := range p.Prompt {
				text += part.Text
			}
			if text == "steering-start" {
				pending = m.ID
				notify(sessionID, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "steering-command", "name": "run_command", "status": "in_progress"})
				notify(sessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "STEERING_READY"}})
				continue
			}
			if pending != nil && strings.HasPrefix(text, "steer-") {
				// agy completes the previous RPC as soon as a running prompt is
				// steered, before the existing command and new response finish.
				reply(pending, map[string]any{"stopReason": "end_turn", "usage": map[string]int{"inputTokens": 10, "outputTokens": 2}})
				pending = m.ID
				notify(sessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "ACCEPTED_" + text}})
				if text == "steer-finish" {
					notify(sessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "steering-command", "status": "completed"})
					reply(pending, map[string]any{"stopReason": "end_turn", "usage": map[string]int{"inputTokens": 7, "outputTokens": 3}})
					pending = nil
				}
				continue
			}
			if strings.Contains(text, "permission-test") || strings.HasSuffix(text, "host-permission-tools") || strings.HasSuffix(text, "host-permission-call") {
				pending = m.ID
				name := "pudding_fake_native"
				if strings.HasSuffix(text, "host-permission-tools") {
					name = "mcp_" + hostName + "_pudding_tools"
				} else if strings.HasSuffix(text, "host-permission-call") {
					name = "mcp_" + hostName + "_pudding_call"
				}
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": "permission", "method": "session/request_permission", "params": map[string]any{"sessionId": sessionID, "toolCall": map[string]any{"toolCallId": "fixture-call", "title": "mcp_" + hostName + "_pudding_call", "name": name, "rawInput": map[string]string{"command": "echo fixture"}}, "options": []any{map[string]string{"optionId": "allow-this", "name": "Allow this operation", "kind": "allow_once"}, map[string]string{"optionId": "deny-this", "name": "Deny this operation", "kind": "reject_once"}}}})
				continue
			}
			if text == "crash" {
				os.Exit(7)
			}
			if strings.HasSuffix(text, "hold") {
				pending = m.ID
				notify(sessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "HOLD_READY"}})
				continue
			}
			if strings.HasSuffix(text, "tool-update-raw-input") {
				notify(sessionID, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "tool-1", "name": "read_file", "title": "Read file", "kind": "read", "status": "pending"})
				notify(sessionID, map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]string{"type": "text", "text": "Inspecting target file"}})
				notify(sessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool-1", "status": "in_progress", "rawInput": map[string]any{"path": "OLD.md", "obsolete": true}})
				notify(sessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool-1", "status": "in_progress", "rawInput": map[string]any{"path": "README.md", "startLine": 1}})
				notify(sessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool-1", "status": "in_progress", "rawInput": nil})
				notify(sessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool-1", "status": "completed"})
				notify(sessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "done"}})
				reply(m.ID, map[string]any{"stopReason": "end_turn"})
				continue
			}
			if strings.HasSuffix(text, "tool-update-cancel") || strings.HasSuffix(text, "tool-update-cancel-failed") {
				pending = m.ID
				failedToolOnCancel = strings.HasSuffix(text, "tool-update-cancel-failed")
				notify(sessionID, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "tool-cancel", "name": "read_file", "status": "pending"})
				notify(sessionID, map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]string{"type": "text", "text": "Still working"}})
				notify(sessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool-cancel", "status": "in_progress", "rawInput": map[string]any{"path": "draft.md", "obsolete": true}})
				notify(sessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool-cancel", "status": "in_progress", "rawInput": map[string]any{"path": "final.md"}})
				notify(sessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool-cancel", "status": "in_progress", "rawInput": nil})
				notify(sessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "CANCEL_READY"}})
				continue
			}
			if strings.HasSuffix(text, "tool-late-failure") {
				notify(sessionID, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "slow-command", "name": "run_command", "status": "in_progress", "rawInput": map[string]string{"command": "search"}})
				notify(sessionID, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "quick-read", "name": "read_file", "status": "completed", "rawInput": map[string]string{"path": "README.md"}})
				notify(sessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "Answer before background command finishes"}})
				notify(sessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "slow-command", "status": "failed", "rawOutput": "terminated"})
				reply(m.ID, map[string]any{"stopReason": "end_turn"})
				continue
			}
			notify(sessionID, map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]string{"type": "text", "text": "Thinking"}})
			notify(sessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": model + "/" + effort + ": " + text}})
			notify(sessionID, map[string]any{"sessionUpdate": "usage_update", "used": 123, "size": 10000})
			reply(m.ID, map[string]any{"stopReason": "end_turn", "usage": map[string]int{"inputTokens": 20, "outputTokens": 7, "thoughtTokens": 3}})
		case "session/cancel":
			if pending != nil {
				if failedToolOnCancel {
					notify(sessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "tool-cancel", "status": "failed", "rawOutput": nil})
					failedToolOnCancel = false
				}
				reply(pending, map[string]string{"stopReason": "cancelled"})
				pending = nil
			}
		case "session/close":
			reply(m.ID, map[string]any{})
		}
	}
}
