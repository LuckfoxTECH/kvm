package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Tool call audit log.
//
// A HID control surface is only useful if the log can answer "who typed what,
// when". Every tool call is therefore logged with its name, a bounded argument
// summary, the outcome and how long it took. The summary is deliberately lossy:
// a typed string is the one argument that may hold a password, so only its
// length and a short prefix are kept, and image data never reaches the log.

// auditPreviewRunes bounds every argument value in the log.
const auditPreviewRunes = 16

// arguments whose content must not be logged verbatim
var sensitiveArgs = map[string]bool{
	"text": true,
}

// summarizeArgs renders tool arguments as a sorted, bounded, single-line string.
func summarizeArgs(args map[string]interface{}) string {
	if len(args) == 0 {
		return ""
	}

	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+summarizeArg(key, args[key]))
	}
	return strings.Join(parts, " ")
}

func summarizeArg(key string, value interface{}) string {
	if sensitiveArgs[key] {
		if text, ok := value.(string); ok {
			return fmt.Sprintf("len=%d %s", len([]rune(text)), quotePrefix(text))
		}
	}

	switch v := value.(type) {
	case string:
		if len([]rune(v)) > auditPreviewRunes {
			return quotePrefix(v) + "..."
		}
		return fmt.Sprintf("%q", v)
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case []interface{}:
		items := make([]string, 0, len(v))
		for _, item := range v {
			items = append(items, summarizeArg("", item))
		}
		return "[" + strings.Join(items, ",") + "]"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func quotePrefix(text string) string {
	runes := []rune(text)
	if len(runes) > auditPreviewRunes {
		runes = runes[:auditPreviewRunes]
	}
	return fmt.Sprintf("%q", string(runes))
}

// auditTool wraps a tool handler so every call leaves a trace. The wrapper
// never changes the outcome: a panic still propagates and an error result is
// passed through untouched.
func auditTool(name string, next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (result *mcp.CallToolResult, err error) {
		start := time.Now()
		defer func() {
			duration := time.Since(start)
			event := log().Info()
			status := "ok"
			message := ""
			switch {
			case err != nil:
				status = "error"
				message = err.Error()
				event = log().Warn()
			case result != nil && result.IsError:
				status = "tool_error"
				message = resultText(result)
				event = log().Warn()
			}
			args := summarizeArgs(request.GetArguments())

			event.
				Str("tool", name).
				Str("args", args).
				Str("status", status).
				Str("message", message).
				Dur("duration", duration).
				Msg("MCP tool call")

			appendAudit(auditEntry{
				Time:     auditTimestamp(),
				Tool:     name,
				Args:     args,
				Status:   status,
				Message:  message,
				Duration: auditDuration(duration),
			})
		}()
		return next(ctx, request)
	}
}

// resultText flattens a tool error result into one log-friendly line.
func resultText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	var parts []string
	for _, content := range result.Content {
		if text, ok := content.(mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, " ")
}
