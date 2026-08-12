package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
)

const (
	codexMultiAgentV2Namespace           = "collaboration"
	codexMultiAgentV2OptimizedNamespace  = "collaboration-optimize"
	codexMultiAgentV2OptimizedNamePrefix = codexMultiAgentV2OptimizedNamespace + "__"
	codexMultiAgentV2ContextKey          = "openai_codex_multi_agent_v2_optimized"
)

var codexMultiAgentV2ToolNames = map[string]struct{}{
	"spawn_agent":   {},
	"send_message":  {},
	"followup_task": {},
}

// CodexMultiAgentV2Enabled reports whether this request is eligible for the
// opt-in compatibility layer. The client gate intentionally stays strict:
// normal OpenAI Responses callers must not receive Codex-private rewrites.
func CodexMultiAgentV2Enabled(cfg *config.Config, c *gin.Context) bool {
	if cfg == nil || !cfg.Gateway.CodexMultiAgentV2Enabled || c == nil {
		return false
	}
	return openai.IsCodexOfficialClientByHeaders(c.GetHeader("User-Agent"), c.GetHeader("originator"))
}

func prepareCodexMultiAgentV2Request(cfg *config.Config, c *gin.Context, body []byte) ([]byte, bool, error) {
	if !CodexMultiAgentV2Enabled(cfg, c) || len(body) == 0 || !json.Valid(body) {
		return body, false, nil
	}

	var requestBody map[string]any
	if err := json.Unmarshal(body, &requestBody); err != nil {
		return body, false, nil
	}
	if requestBody == nil {
		return body, false, nil
	}

	optimizedNamespace := codexMultiAgentV2ShouldOptimizeNamespace(requestBody)
	changed := rewriteCodexMultiAgentV2Value(requestBody, optimizedNamespace)
	if !changed {
		return body, false, nil
	}

	rebuilt, err := marshalOpenAIUpstreamJSON(requestBody)
	if err != nil {
		return body, false, fmt.Errorf("encode Codex multi-agent v2 request: %w", err)
	}
	if optimizedNamespace {
		c.Set(codexMultiAgentV2ContextKey, true)
	}
	return rebuilt, true, nil
}

func codexMultiAgentV2ShouldOptimizeNamespace(requestBody map[string]any) bool {
	if codexMultiAgentV2HasNamespace(requestBody, codexMultiAgentV2OptimizedNamespace) {
		return false
	}
	return codexMultiAgentV2HasSpawnAgentInNamespace(requestBody, codexMultiAgentV2Namespace)
}

func codexMultiAgentV2HasNamespace(value any, wanted string) bool {
	found := false
	walkCodexMultiAgentV2Value(value, func(item map[string]any) {
		if strings.TrimSpace(codexMultiAgentV2StringValue(item["type"])) == "namespace" && strings.TrimSpace(codexMultiAgentV2StringValue(item["name"])) == wanted {
			found = true
		}
	})
	return found
}

func codexMultiAgentV2HasSpawnAgentInNamespace(value any, namespace string) bool {
	found := false
	walkCodexMultiAgentV2Value(value, func(item map[string]any) {
		if strings.TrimSpace(codexMultiAgentV2StringValue(item["type"])) != "namespace" || strings.TrimSpace(codexMultiAgentV2StringValue(item["name"])) != namespace {
			return
		}
		for _, child := range codexMultiAgentV2NamespaceChildren(item) {
			childMap, ok := child.(map[string]any)
			if !ok {
				continue
			}
			if strings.TrimSpace(codexMultiAgentV2StringValue(childMap["name"])) == "spawn_agent" {
				found = true
				return
			}
		}
	})
	return found
}

func codexMultiAgentV2NamespaceChildren(item map[string]any) []any {
	if children, ok := item["tools"].([]any); ok {
		return children
	}
	if children, ok := item["children"].([]any); ok {
		return children
	}
	return nil
}

func rewriteCodexMultiAgentV2Value(value any, optimizeNamespace bool) bool {
	changed := false
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			changed = rewriteCodexMultiAgentV2Value(item, optimizeNamespace) || changed
		}
	case map[string]any:
		toolName := strings.TrimSpace(codexMultiAgentV2StringValue(typed["name"]))
		if _, ok := codexMultiAgentV2ToolNames[toolName]; ok {
			changed = removeCodexMultiAgentV2MessageEncryption(typed) || changed
		}
		if optimizeNamespace {
			if strings.TrimSpace(codexMultiAgentV2StringValue(typed["type"])) == "namespace" && toolName == codexMultiAgentV2Namespace {
				typed["name"] = codexMultiAgentV2OptimizedNamespace
				changed = true
			}
			if strings.TrimSpace(codexMultiAgentV2StringValue(typed["namespace"])) == codexMultiAgentV2Namespace {
				typed["namespace"] = codexMultiAgentV2OptimizedNamespace
				changed = true
			}
		}
		if strings.TrimSpace(codexMultiAgentV2StringValue(typed["type"])) == "agent_message" {
			changed = normalizeCodexMultiAgentV2AgentMessage(typed) || changed
		}
		for _, item := range typed {
			changed = rewriteCodexMultiAgentV2Value(item, optimizeNamespace) || changed
		}
	}
	return changed
}

func walkCodexMultiAgentV2Value(value any, visit func(map[string]any)) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			walkCodexMultiAgentV2Value(item, visit)
		}
	case map[string]any:
		visit(typed)
		for _, item := range typed {
			walkCodexMultiAgentV2Value(item, visit)
		}
	}
}

func removeCodexMultiAgentV2MessageEncryption(tool map[string]any) bool {
	parameters, ok := tool["parameters"].(map[string]any)
	if !ok {
		return false
	}
	properties, ok := parameters["properties"].(map[string]any)
	if !ok {
		return false
	}
	message, ok := properties["message"].(map[string]any)
	if !ok {
		return false
	}
	if _, exists := message["encrypted"]; !exists {
		return false
	}
	delete(message, "encrypted")
	return true
}

func normalizeCodexMultiAgentV2AgentMessage(item map[string]any) bool {
	content, ok := item["content"].([]any)
	if !ok {
		return false
	}
	changed := false
	for _, rawPart := range content {
		part, ok := rawPart.(map[string]any)
		if !ok || strings.TrimSpace(codexMultiAgentV2StringValue(part["type"])) != "encrypted_content" {
			continue
		}
		encryptedContent, exists := part["encrypted_content"]
		if !exists {
			continue
		}
		part["type"] = "input_text"
		part["text"] = encryptedContent
		delete(part, "encrypted_content")
		changed = true
	}
	return changed
}

func restoreCodexMultiAgentV2Response(c *gin.Context, payload []byte) ([]byte, error) {
	if !codexMultiAgentV2RequestOptimized(c) || len(payload) == 0 || !json.Valid(payload) {
		return payload, nil
	}

	var response any
	if err := json.Unmarshal(payload, &response); err != nil {
		return payload, err
	}
	if !restoreCodexMultiAgentV2Value(response) {
		return payload, nil
	}
	rebuilt, err := marshalOpenAIUpstreamJSON(response)
	if err != nil {
		return payload, fmt.Errorf("encode Codex multi-agent v2 response: %w", err)
	}
	return rebuilt, nil
}

func codexMultiAgentV2RequestOptimized(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(codexMultiAgentV2ContextKey)
	optimized, _ := value.(bool)
	return ok && optimized
}

func restoreCodexMultiAgentV2Value(value any) bool {
	changed := false
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			changed = restoreCodexMultiAgentV2Value(item) || changed
		}
	case map[string]any:
		if strings.TrimSpace(codexMultiAgentV2StringValue(typed["type"])) == "namespace" && strings.TrimSpace(codexMultiAgentV2StringValue(typed["name"])) == codexMultiAgentV2OptimizedNamespace {
			typed["name"] = codexMultiAgentV2Namespace
			changed = true
		}
		if strings.TrimSpace(codexMultiAgentV2StringValue(typed["namespace"])) == codexMultiAgentV2OptimizedNamespace {
			typed["namespace"] = codexMultiAgentV2Namespace
			changed = true
		}
		if typ := strings.TrimSpace(codexMultiAgentV2StringValue(typed["type"])); typ == "function_call" || typ == "custom_tool_call" {
			if name := codexMultiAgentV2StringValue(typed["name"]); strings.HasPrefix(name, codexMultiAgentV2OptimizedNamePrefix) {
				typed["name"] = codexMultiAgentV2Namespace + "__" + strings.TrimPrefix(name, codexMultiAgentV2OptimizedNamePrefix)
				changed = true
			}
		}
		for _, item := range typed {
			changed = restoreCodexMultiAgentV2Value(item) || changed
		}
	}
	return changed
}

func codexMultiAgentV2StringValue(value any) string {
	text, _ := value.(string)
	return text
}

// AugmentCodexModelsManifestForMultiAgentV2 advertises the v2 capability to
// Codex clients while leaving unknown/evolving manifest fields untouched.
func AugmentCodexModelsManifestForMultiAgentV2(body []byte) ([]byte, bool) {
	var manifest map[string]any
	if len(body) == 0 || json.Unmarshal(body, &manifest) != nil || manifest == nil {
		return body, false
	}
	models, ok := manifest["models"].([]any)
	if !ok {
		return body, false
	}
	changed := false
	for _, rawModel := range models {
		model, ok := rawModel.(map[string]any)
		if !ok {
			continue
		}
		if _, exists := model["multi_agent_version"]; exists {
			continue
		}
		model["multi_agent_version"] = "v2"
		changed = true
	}
	if !changed {
		return body, false
	}
	rebuilt, err := marshalOpenAIUpstreamJSON(manifest)
	if err != nil {
		return body, false
	}
	return rebuilt, true
}
