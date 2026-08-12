package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newCodexMultiAgentV2TestContext(t *testing.T, official bool) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	if official {
		c.Request.Header.Set("User-Agent", "codex_cli_rs/0.1.0")
	}
	return c
}

func TestPrepareCodexMultiAgentV2Request(t *testing.T) {
	c := newCodexMultiAgentV2TestContext(t, true)
	cfg := &config.Config{Gateway: config.GatewayConfig{CodexMultiAgentV2Enabled: true}}
	body := []byte(`{"model":"gpt-5.6-luna","tools":[{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"spawn_agent","parameters":{"type":"object","properties":{"message":{"type":"string","encrypted":true}}}},{"type":"function","name":"send_message","parameters":{"type":"object","properties":{"message":{"type":"string","encrypted":true}}}}]}],"tool_choice":{"type":"function","name":"spawn_agent","namespace":"collaboration"},"input":[{"type":"function_call","name":"spawn_agent","namespace":"collaboration","arguments":"{}"},{"type":"agent_message","content":[{"type":"encrypted_content","encrypted_content":"sealed"}]}]}`)

	got, changed, err := prepareCodexMultiAgentV2Request(cfg, c, body)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, codexMultiAgentV2RequestOptimized(c))
	require.Equal(t, codexMultiAgentV2OptimizedNamespace, gjson.GetBytes(got, "tools.0.name").String())
	require.Equal(t, codexMultiAgentV2OptimizedNamespace, gjson.GetBytes(got, "tool_choice.namespace").String())
	require.Equal(t, codexMultiAgentV2OptimizedNamespace, gjson.GetBytes(got, "input.0.namespace").String())
	require.False(t, gjson.GetBytes(got, "tools.0.tools.0.parameters.properties.message.encrypted").Exists())
	require.Equal(t, "input_text", gjson.GetBytes(got, "input.1.content.0.type").String())
	require.Equal(t, "sealed", gjson.GetBytes(got, "input.1.content.0.text").String())
	require.False(t, gjson.GetBytes(got, "input.1.content.0.encrypted_content").Exists())
}

func TestPrepareCodexMultiAgentV2RequestSkipsNonOfficialClients(t *testing.T) {
	c := newCodexMultiAgentV2TestContext(t, false)
	cfg := &config.Config{Gateway: config.GatewayConfig{CodexMultiAgentV2Enabled: true}}
	body := []byte(`{"tools":[{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"spawn_agent","parameters":{"properties":{"message":{"encrypted":true}}}}]}]}`)

	got, changed, err := prepareCodexMultiAgentV2Request(cfg, c, body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, string(body), string(got))
	require.False(t, codexMultiAgentV2RequestOptimized(c))
}

func TestPrepareCodexMultiAgentV2RequestKeepsNamespaceOnConflict(t *testing.T) {
	c := newCodexMultiAgentV2TestContext(t, true)
	cfg := &config.Config{Gateway: config.GatewayConfig{CodexMultiAgentV2Enabled: true}}
	body := []byte(`{"tools":[{"type":"namespace","name":"collaboration-optimize","tools":[]},{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"spawn_agent","parameters":{"properties":{"message":{"encrypted":true}}}}]}]}`)

	got, changed, err := prepareCodexMultiAgentV2Request(cfg, c, body)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, codexMultiAgentV2RequestOptimized(c))
	require.Equal(t, "collaboration", gjson.GetBytes(got, "tools.1.name").String())
	require.False(t, gjson.GetBytes(got, "tools.1.tools.0.parameters.properties.message.encrypted").Exists())
}

func TestRestoreCodexMultiAgentV2ResponseAfterNamespaceFlattening(t *testing.T) {
	c := newCodexMultiAgentV2TestContext(t, true)
	c.Set(codexMultiAgentV2ContextKey, true)
	c.Set(openAIResponsesNamespaceNamesContextKey, map[string]apicompat.ResponsesNamespaceName{
		"collaboration-optimize__spawn_agent": {Namespace: codexMultiAgentV2OptimizedNamespace, Name: "spawn_agent"},
	})
	payload := []byte(`{"type":"response.completed","response":{"output":[{"type":"function_call","name":"collaboration-optimize__spawn_agent","arguments":"{}"},{"type":"function_call","name":"send_message","namespace":"collaboration-optimize","arguments":"{}"}]}}`)

	got, err := restoreOpenAIResponsesNamespacePayload(c, payload)
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"response.completed","response":{"output":[{"type":"function_call","name":"spawn_agent","namespace":"collaboration","arguments":"{}"},{"type":"function_call","name":"send_message","namespace":"collaboration","arguments":"{}"}]}}`, string(got))
}

func TestAugmentCodexModelsManifestForMultiAgentV2(t *testing.T) {
	body := []byte(`{"models":[{"slug":"gpt-5.6-luna"},{"slug":"gpt-5.6-sol","multi_agent_version":"legacy"}],"next_page":null}`)

	got, changed := AugmentCodexModelsManifestForMultiAgentV2(body)
	require.True(t, changed)
	require.Equal(t, "v2", gjson.GetBytes(got, "models.0.multi_agent_version").String())
	require.Equal(t, "legacy", gjson.GetBytes(got, "models.1.multi_agent_version").String())
	require.Nil(t, gjson.GetBytes(got, "next_page").Value())
}
