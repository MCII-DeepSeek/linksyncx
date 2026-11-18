//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type tlsCapturingUpstreamStub struct {
	doCalls        int
	doWithTLSCalls int
	lastURL        string
	lastProfile    *tlsfingerprint.Profile
	lastHeader     http.Header
	lastBody       []byte
	responseBody   string
}

func (u *tlsCapturingUpstreamStub) record(req *http.Request, profile *tlsfingerprint.Profile) (*http.Response, error) {
	if req.URL != nil {
		u.lastURL = req.URL.String()
	}
	u.lastProfile = profile
	u.lastHeader = req.Header.Clone()
	if req.Body != nil {
		u.lastBody, _ = io.ReadAll(req.Body)
	}
	body := u.responseBody
	if body == "" {
		body = "data: {\"response\":{\"responseId\":\"resp_1\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"pong\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":2}}}\n\n"
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func (u *tlsCapturingUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.doCalls++
	return u.record(req, nil)
}

func (u *tlsCapturingUpstreamStub) DoWithTLS(req *http.Request, _ string, _ int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.doWithTLSCalls++
	return u.record(req, profile)
}

func TestAntigravityGateway_TLSFingerprintSessionMaskingAndHeaderOverrides(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tlsSvc := &TLSFingerprintProfileService{}
	tlsSvc.setLocalCache([]*model.TLSFingerprintProfile{
		{
			ID:             7,
			Name:           "macOS Antigravity Electron",
			Curves:         []uint16{29},
			KeyShareGroups: []uint16{29},
		},
	})

	upstream := &tlsCapturingUpstreamStub{}
	tokenProvider := NewAntigravityTokenProvider(nil, nil, nil)
	settingSvc := NewSettingService(&antigravitySettingRepoStub{}, &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}})
	svc := NewAntigravityGatewayService(nil, nil, nil, tokenProvider, nil, upstream, settingSvc, nil)
	svc.SetTLSFingerprintProfileService(tlsSvc)

	oauthAccount := &Account{
		ID:          88,
		Name:        "ag-oauth-darwin",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Concurrency: 2,
		Credentials: map[string]any{
			"access_token": "tok-valid",
			"expires_at":   time.Now().Add(time.Hour).Format(time.RFC3339),
			"project_id":   "proj-88",
			"device_os":    "MacOS",
			"device_arch":  "arm64",
		},
		Extra: map[string]any{
			"enable_tls_fingerprint":     true,
			"tls_fingerprint_profile_id": float64(7),
			"session_id_masking_enabled": true,
		},
	}

	// 1. TestConnection：验证同时注入 darwin/arm64 设备画像与绑定的 TLS 指纹模板
	testRes, err := svc.TestConnection(context.Background(), oauthAccount, "claude-sonnet-4-6", "ping")
	require.NoError(t, err)
	require.Equal(t, "pong", testRes.Text)
	require.Equal(t, 1, upstream.doWithTLSCalls)
	require.NotNil(t, upstream.lastProfile)
	require.Equal(t, "macOS Antigravity Electron", upstream.lastProfile.Name)
	require.True(t, strings.HasSuffix(upstream.lastHeader.Get("User-Agent"), " darwin/arm64"))
	require.Contains(t, upstream.lastHeader.Get("Client-Metadata"), `"platform":"MACOS"`)

	// 2. Forward (Claude OAuth)：验证剥离下游 Claude Code 指纹并按 session_id_masking 收敛同一账号会话
	bodyClientA := []byte(`{"model":"claude-sonnet-4-6","max_tokens":16,"stream":false,"metadata":{"user_id":"user_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_account_11111111-1111-1111-1111-111111111111_session_22222222-2222-2222-2222-222222222222"},"messages":[{"role":"user","content":"hi"}]}`)
	recA := httptest.NewRecorder()
	cA, _ := gin.CreateTestContext(recA)
	cA.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyClientA))
	_, err = svc.Forward(context.Background(), cA, oauthAccount, bodyClientA, false)
	require.NoError(t, err)
	sidA := gjson.GetBytes(upstream.lastBody, "request.sessionId").String()
	require.NotEmpty(t, sidA)
	require.NotContains(t, sidA, "user_")
	require.NotContains(t, sidA, "account_")

	bodyClientB := []byte(`{"model":"claude-sonnet-4-6","max_tokens":16,"stream":false,"metadata":{"user_id":"user_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb_account_33333333-3333-3333-3333-333333333333_session_44444444-4444-4444-4444-444444444444"},"messages":[{"role":"user","content":"other"}]}`)
	recB := httptest.NewRecorder()
	cB, _ := gin.CreateTestContext(recB)
	cB.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(bodyClientB))
	_, err = svc.Forward(context.Background(), cB, oauthAccount, bodyClientB, false)
	require.NoError(t, err)
	sidB := gjson.GetBytes(upstream.lastBody, "request.sessionId").String()
	require.Equal(t, sidA, sidB, "session_id_masking_enabled should converge distinct clients onto one masked sessionId")

	// 3. ForwardGemini：验证单遍转换保留身份补丁、清洗 Schema 且走 DoWithTLS
	geminiBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"tools":[{"functionDeclarations":[{"name":"fn","parameters":{"type":"object","$schema":"https://json-schema.org/draft/2020-12/schema","properties":{"q":{"type":"string"}}}}]}]}`)
	recG := httptest.NewRecorder()
	cG, _ := gin.CreateTestContext(recG)
	cG.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", bytes.NewReader(geminiBody))
	_, err = svc.ForwardGemini(context.Background(), cG, oauthAccount, "gemini-2.5-flash", "generateContent", false, geminiBody, false)
	require.NoError(t, err)
	require.Equal(t, 4, upstream.doWithTLSCalls)
	require.Contains(t, gjson.GetBytes(upstream.lastBody, "request.systemInstruction.parts.0.text").String(), "You are Antigravity")
	require.False(t, gjson.GetBytes(upstream.lastBody, "request.tools.0.functionDeclarations.0.parameters.$schema").Exists())

	// 4. ForwardUpstream：验证中转账号同时生效 TLS 指纹与请求头覆写（保护认证头不被篡改）
	relayUpstream := &tlsCapturingUpstreamStub{
		responseBody: `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":4,"output_tokens":2}}`,
	}
	relaySvc := NewAntigravityGatewayService(nil, nil, nil, tokenProvider, nil, relayUpstream, settingSvc, nil)
	relaySvc.SetTLSFingerprintProfileService(tlsSvc)
	relayAccount := &Account{
		ID:          99,
		Name:        "ag-upstream-relay",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeUpstream,
		Concurrency: 2,
		Credentials: map[string]any{
			"base_url":                   "https://relay.example.com",
			"api_key":                    "sk-real-relay",
			credKeyHeaderOverrideEnabled: true,
			credKeyHeaderOverrides: map[string]any{
				"User-Agent":    "CustomRelayClient/3.0",
				"X-Relay-Gate":  "pass-token",
				"Authorization": "Bearer forged", // 黑名单头必须被拦截
			},
		},
		Extra: map[string]any{
			"enable_tls_fingerprint":     true,
			"tls_fingerprint_profile_id": float64(7),
		},
	}
	recU := httptest.NewRecorder()
	cU, _ := gin.CreateTestContext(recU)
	relayBody := []byte(`{"model":"claude-sonnet-4-6","max_tokens":16,"stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	cU.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(relayBody))
	_, err = relaySvc.Forward(context.Background(), cU, relayAccount, relayBody, false)
	require.NoError(t, err)
	require.Equal(t, 1, relayUpstream.doWithTLSCalls)
	require.Equal(t, "CustomRelayClient/3.0", relayUpstream.lastHeader.Get("User-Agent"))
	require.Equal(t, "pass-token", getHeaderRaw(relayUpstream.lastHeader, "x-relay-gate"))
	require.Equal(t, "Bearer sk-real-relay", relayUpstream.lastHeader.Get("Authorization"))
}

func TestResolveTLSProfile_DeviceOSAffinity(t *testing.T) {
	svc := &TLSFingerprintProfileService{}
	svc.setLocalCache([]*model.TLSFingerprintProfile{
		{ID: 1, Name: "macOS Electron 35", Curves: []uint16{29}, KeyShareGroups: []uint16{29}},
		{ID: 2, Name: "Windows 11 Node.js", Curves: []uint16{29}, KeyShareGroups: []uint16{29}},
		{ID: 3, Name: "Linux Ubuntu Client", Curves: []uint16{29}, KeyShareGroups: []uint16{29}},
	})

	linuxAccount := &Account{
		ID:       10,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"device_os":   "Linux",
			"device_arch": "x64",
		},
		Extra: map[string]any{
			"enable_tls_fingerprint":     true,
			"tls_fingerprint_profile_id": float64(-1),
		},
	}
	darwinAccount := &Account{
		ID:       10, // 同一个账号 ID，仅设备画像改为 MacOS
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"device_os":   "MacOS",
			"device_arch": "arm64",
		},
		Extra: map[string]any{
			"enable_tls_fingerprint":     true,
			"tls_fingerprint_profile_id": float64(-1),
		},
	}

	require.Equal(t, "Linux Ubuntu Client", svc.ResolveTLSProfile(linuxAccount).Name)
	require.Equal(t, "macOS Electron 35", svc.ResolveTLSProfile(darwinAccount).Name)
}

func TestAntigravityAuditFixes_NilReceiverH2ALPNCompatSessionAndTestRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oauthAcc := &Account{
		ID:       501,
		Name:     "ag-oauth-h2",
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"enable_tls_fingerprint":     true,
			"session_id_masking_enabled": false,
		},
	}
	relayAcc := &Account{
		ID:       502,
		Name:     "ag-upstream-relay",
		Platform: PlatformAntigravity,
		Type:     AccountTypeUpstream,
		Credentials: map[string]any{
			"base_url":                   "https://relay.example.com",
			"api_key":                    "sk-upstream-test",
			credKeyHeaderOverrideEnabled: true,
			credKeyHeaderOverrides: map[string]any{
				"X-Relay-Header": "relay-ok",
			},
		},
		Extra: map[string]any{
			"enable_tls_fingerprint": true,
		},
	}

	// 修复 3 & 修复 2：nil 接收者安全不 panic；OAuth 自动补齐 h2 ALPN，Relay 保持默认 http/1.1
	var nilGatewaySvc *AntigravityGatewayService
	oauthProfile := nilGatewaySvc.resolveTLSProfile(oauthAcc)
	require.NotNil(t, oauthProfile)
	require.Equal(t, []string{"h2", "http/1.1"}, oauthProfile.ALPNProtocols)
	relayProfile := nilGatewaySvc.resolveTLSProfile(relayAcc)
	require.NotNil(t, relayProfile)
	require.Empty(t, relayProfile.ALPNProtocols)

	// loadAntigravityRelayModelsWithDeps 在 gatewaySvc == nil 时也不 panic 且携带 HeaderOverrides + DoWithTLS
	relayModelsUpstream := &tlsCapturingUpstreamStub{
		responseBody: `{"data":[{"id":"claude-sonnet-4-6","type":"model"}]}`,
	}
	models, err := loadAntigravityRelayModelsWithDeps(
		context.Background(),
		relayAcc,
		nil,
		relayModelsUpstream,
		nil,
		func(raw string) (string, error) { return raw, nil },
		nil,
	)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, 1, relayModelsUpstream.doWithTLSCalls)
	require.Equal(t, "relay-ok", getHeaderRaw(relayModelsUpstream.lastHeader, "x-relay-header"))

	// 修复 7：Claude 兼容请求路由到 gemini-* 模型时，剥离 metadata.user_id 指纹并注入官方格式 sessionId
	claudeCompatBody := []byte(`{
		"model":"gemini-2.5-pro",
		"max_tokens":32,
		"metadata":{"user_id":"user_abcdef123456_account_11111111-2222-3333-4444-555555555555_session_99999999-aaaa-bbbb-cccc-dddddddddddd"},
		"messages":[{"role":"user","content":"hello gemini"}]
	}`)
	var claudeReq antigravity.ClaudeRequest
	require.NoError(t, json.Unmarshal(claudeCompatBody, &claudeReq))
	svc := &AntigravityGatewayService{}
	wrappedCompat, err := svc.buildAntigravityCompatGeminiBodyForAccount(
		context.Background(),
		nil,
		oauthAcc,
		claudeCompatBody,
		&claudeReq,
		"proj-501",
		"gemini-2.5-pro",
	)
	require.NoError(t, err)
	compatSID := gjson.GetBytes(wrappedCompat, "request.sessionId").String()
	require.True(t, strings.HasPrefix(compatSID, "-"), "expected canonical sessionId, got %q", compatSID)
	require.NotContains(t, compatSID, "user_")
	require.NotContains(t, compatSID, "account_")

	// 原生 Gemini 裸请求默认不额外注入 sessionId，但显式携带下游会话头时注入
	bareGeminiMap, err := prepareGeminiRequestPayload([]byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`), oauthAcc, "", false)
	require.NoError(t, err)
	_, hasBareSID := bareGeminiMap["sessionId"]
	require.False(t, hasBareSID)

	hintedGeminiMap, err := prepareGeminiRequestPayload([]byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`), oauthAcc, "sess-header-123", false)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(hintedGeminiMap["sessionId"].(string), "-"))

	// 修复 5：AccountTestService 对 Antigravity Upstream 走 /v1/messages 中转测活，且 APIKey Gemini 测活应用 HeaderOverrides
	testUpstream := &tlsCapturingUpstreamStub{
		responseBody: "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	testSvc := &AccountTestService{
		httpUpstream: testUpstream,
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{Enabled: false},
			},
		},
		tlsFPProfileService: &TLSFingerprintProfileService{},
	}
	rec := httptest.NewRecorder()
	cTest, _ := gin.CreateTestContext(rec)
	cTest.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/502/test", nil)
	require.NoError(t, testSvc.routeAntigravityTest(cTest, relayAcc, "claude-sonnet-4-6", "ping"))
	require.Equal(t, 1, testUpstream.doWithTLSCalls)
	require.Equal(t, "https://relay.example.com/v1/messages", testUpstream.lastURL)
	require.Equal(t, "relay-ok", getHeaderRaw(testUpstream.lastHeader, "x-relay-header"))
	require.Equal(t, "Bearer sk-upstream-test", testUpstream.lastHeader.Get("Authorization"))
	require.Contains(t, rec.Body.String(), `"test_complete"`)

	apiKeyAcc := &Account{
		ID:       503,
		Platform: PlatformAntigravity,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url":                   "https://gemini-relay.example.com",
			"api_key":                    "AIza-relay",
			credKeyHeaderOverrideEnabled: true,
			credKeyHeaderOverrides: map[string]any{
				"X-Gemini-Gate": "gate-1",
			},
		},
	}
	geminiReq, err := testSvc.buildGeminiAPIKeyRequest(context.Background(), apiKeyAcc, "gemini-2.5-flash", []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, "gate-1", getHeaderRaw(geminiReq.Header, "x-gemini-gate"))
	require.Contains(t, geminiReq.URL.String(), "https://gemini-relay.example.com/antigravity/v1beta/models/gemini-2.5-flash:streamGenerateContent")
}

type deadlineTrackingResponseWriter struct {
	gin.ResponseWriter
	currentDeadline  time.Time
	deadlineAtFlush  time.Time
	flushCalledCount int
}

func (w *deadlineTrackingResponseWriter) SetWriteDeadline(t time.Time) error {
	w.currentDeadline = t
	return nil
}

func (w *deadlineTrackingResponseWriter) Flush() {
	_ = w.FlushError()
}

func (w *deadlineTrackingResponseWriter) FlushError() error {
	w.flushCalledCount++
	w.deadlineAtFlush = w.currentDeadline
	return nil
}

func TestAuditP1P2Fixes_WriteDeadline_GeminiCompatTLSAndHeaders_ClaudeH2Guard_SessionScope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1) [P1-1] 流式写超时必须完整覆盖 Flush()，并在 Flush 完成后才恢复为零值
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	trackingW := &deadlineTrackingResponseWriter{ResponseWriter: c.Writer}
	cw := newAntigravityClientWriter(trackingW, trackingW, "test-sse")
	require.True(t, cw.WriteSSEData([]byte(`{"ok":true}`)))
	require.Equal(t, 1, trackingW.flushCalledCount)
	require.False(t, trackingW.deadlineAtFlush.IsZero(), "write deadline must still be armed during Flush")
	require.True(t, trackingW.currentDeadline.IsZero(), "write deadline must be cleared after Flush completes")

	require.True(t, cw.WriteLine(": keepalive"))
	require.Equal(t, 2, trackingW.flushCalledCount)
	require.False(t, trackingW.deadlineAtFlush.IsZero(), "write deadline must still be armed during WriteLine Flush")
	require.True(t, trackingW.currentDeadline.IsZero())

	// 2) [P1-3] GeminiMessagesCompatService.ForwardNative 对 Antigravity APIKey / Upstream 账号同步生效 HeaderOverrides 与 DoWithTLS
	tlsSvc := &TLSFingerprintProfileService{}
	tlsSvc.setLocalCache([]*model.TLSFingerprintProfile{
		{
			ID:             11,
			Name:           "Windows 11 Antigravity Client",
			ALPNProtocols:  []string{"h2", "http/1.1"},
			Curves:         []uint16{29},
			KeyShareGroups: []uint16{29},
		},
		{
			ID:             12,
			Name:           "macOS Safari Client",
			ALPNProtocols:  []string{"h2", "http/1.1"},
			Curves:         []uint16{29},
			KeyShareGroups: []uint16{29},
		},
	})

	geminiUpstream := &tlsCapturingUpstreamStub{
		responseBody: `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2}}`,
	}
	allowInsecureCfg := &config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		},
	}
	geminiCompatSvc := NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, geminiUpstream, nil, allowInsecureCfg)
	geminiCompatSvc.SetTLSFingerprintProfileService(tlsSvc)

	agAPIKeyAcc := &Account{
		ID:          601,
		Platform:    PlatformAntigravity,
		Type:        AccountTypeAPIKey,
		Concurrency: 3,
		Credentials: map[string]any{
			"base_url":                   "https://ag-relay.example.com",
			"api_key":                    "AIza-ag-key",
			credKeyHeaderOverrideEnabled: true,
			credKeyHeaderOverrides: map[string]any{
				"X-Custom-Relay": "custom-val",
				"x-goog-api-key": "forged-key", // 黑名单头禁止覆盖真实 api_key
			},
		},
		Extra: map[string]any{
			"enable_tls_fingerprint":     true,
			"tls_fingerprint_profile_id": float64(11),
		},
	}
	recNative := httptest.NewRecorder()
	cNative, _ := gin.CreateTestContext(recNative)
	cNative.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", nil)
	_, err := geminiCompatSvc.ForwardNative(
		context.Background(),
		cNative,
		agAPIKeyAcc,
		"gemini-2.5-flash",
		"generateContent",
		false,
		[]byte(`{"contents":[{"role":"user","parts":[{"text":"ping"}]}]}`),
	)
	require.NoError(t, err)
	require.Equal(t, 1, geminiUpstream.doWithTLSCalls)
	require.Equal(t, "https://ag-relay.example.com/antigravity/v1beta/models/gemini-2.5-flash:generateContent", geminiUpstream.lastURL)
	require.Equal(t, "custom-val", getHeaderRaw(geminiUpstream.lastHeader, "x-custom-relay"))
	require.Equal(t, "AIza-ag-key", geminiUpstream.lastHeader.Get("x-goog-api-key"))

	// 3) [P2-4] Claude Code (Anthropic OAuth) 抽中或绑定含 h2 的模板时自动剥离 h2 ALPN，且 Bedrock 不误开启 TLS 指纹
	claudeOAuth := &Account{
		ID:       701,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"enable_tls_fingerprint":     true,
			"tls_fingerprint_profile_id": float64(11),
		},
	}
	claudeProfile := tlsSvc.ResolveTLSProfile(claudeOAuth)
	require.NotNil(t, claudeProfile)
	require.Equal(t, []string{"http/1.1"}, claudeProfile.ALPNProtocols)
	require.False(t, tlsfingerprint.ProfileSupportsHTTP2(claudeProfile))

	bedrockAcc := &Account{
		ID:       702,
		Platform: PlatformAnthropic,
		Type:     AccountTypeBedrock,
		Extra:    map[string]any{"enable_tls_fingerprint": true},
	}
	require.False(t, bedrockAcc.IsTLSFingerprintEnabled())

	// OpenAI 主转发链路启用 TLS 指纹后走 DoWithTLS
	openAIUpstream := &tlsCapturingUpstreamStub{responseBody: `{}`}
	openAISvc := &OpenAIGatewayService{
		httpUpstream:        openAIUpstream,
		tlsFPProfileService: tlsSvc,
	}
	openAIAcc := &Account{
		ID:       703,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			"enable_tls_fingerprint":     true,
			"tls_fingerprint_profile_id": float64(11),
		},
	}
	openAIReq, _ := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader([]byte(`{}`)))
	openAIResp, err := openAISvc.doOpenAIUpstream(openAIReq, "", openAIAcc)
	require.NoError(t, err)
	_ = openAIResp.Body.Close()
	require.Equal(t, 1, openAIUpstream.doWithTLSCalls)

	// 4) [P2-5] 未显式钉住设备的默认 Antigravity OAuth 账号在随机模式下优先对齐默认 Windows 模板
	defaultAgOAuth := &Account{
		ID:       12, // 即使 ID=12，因默认呈現 Windows 身份也应避开 ID=12 的 macOS 模板而选 Windows 模板
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"enable_tls_fingerprint":     true,
			"tls_fingerprint_profile_id": float64(-1),
			"session_id_masking_enabled": true,
		},
	}
	require.Equal(t, "Windows 11 Antigravity Client", tlsSvc.ResolveTLSProfile(defaultAgOAuth).Name)

	// 5) [P2-6] Antigravity 的 session_id_masking_enabled 不触发 Claude 单会话预算/账号级队列锁
	require.True(t, defaultAgOAuth.IsSessionIDMaskingEnabled())
	require.False(t, defaultAgOAuth.IsSingleSessionModeEnabled())
	umq := NewUserMessageQueueService(nil, nil, nil)
	umqHeaders := http.Header{"X-Claude-Code-Session-Id": []string{"sess-abc"}}
	scope := umq.ScopeFor(defaultAgOAuth, &ParsedRequest{}, umqHeaders)
	require.NotEmpty(t, scope.Session, "Antigravity session masking must not collapse UMQ to account-wide single-session lock")

	// 6) GeminiMessagesCompatService.Forward 对 Antigravity apikey 账号按请求思考预算解析裸 Gemini 模型的思考深度变体
	geminiCompatUpstream := &tlsCapturingUpstreamStub{
		responseBody: `{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2}}`,
	}
	compatSvc := NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, geminiCompatUpstream, nil, allowInsecureCfg)
	compatSvc.SetTLSFingerprintProfileService(tlsSvc)
	agThinkingRelayAcc := &Account{
		ID:          602,
		Platform:    PlatformAntigravity,
		Type:        AccountTypeAPIKey,
		Concurrency: 2,
		Credentials: map[string]any{
			"base_url": "https://ag-relay.example.com",
			"api_key":  "AIza-ag-key",
			"model_mapping": map[string]any{
				"gemini-3.1-pro-low":  "gemini-3.1-pro-low",
				"gemini-3.1-pro-high": "gemini-3.1-pro-high",
			},
		},
	}
	recCompatLow := httptest.NewRecorder()
	cCompatLow, _ := gin.CreateTestContext(recCompatLow)
	compatLowBody := []byte(`{"model":"gemini-3.1-pro","max_tokens":64,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"hello"}]}`)
	cCompatLow.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(compatLowBody))
	_, err = compatSvc.Forward(context.Background(), cCompatLow, agThinkingRelayAcc, compatLowBody)
	require.NoError(t, err)
	require.Contains(t, geminiCompatUpstream.lastURL, "/antigravity/v1beta/models/gemini-3.1-pro-low:generateContent")

	recCompatHigh := httptest.NewRecorder()
	cCompatHigh, _ := gin.CreateTestContext(recCompatHigh)
	compatHighBody := []byte(`{"model":"gemini-3.1-pro","max_tokens":64,"thinking":{"type":"enabled","budget_tokens":16384},"messages":[{"role":"user","content":"hello"}]}`)
	cCompatHigh.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(compatHighBody))
	_, err = compatSvc.Forward(context.Background(), cCompatHigh, agThinkingRelayAcc, compatHighBody)
	require.NoError(t, err)
	require.Contains(t, geminiCompatUpstream.lastURL, "/antigravity/v1beta/models/gemini-3.1-pro-high:generateContent")

	// 7) OpenAI 启用内置默认 TLS 指纹时自动对齐 h2 ALPN，且附属 HTTP 链路同步走 DoWithTLS
	openAIDefaultFPAcc := &Account{
		ID:       704,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"enable_tls_fingerprint": true},
	}
	resolvedOpenAIDefault := tlsSvc.ResolveTLSProfile(openAIDefaultFPAcc)
	require.NotNil(t, resolvedOpenAIDefault)
	require.Equal(t, []string{"h2", "http/1.1"}, resolvedOpenAIDefault.ALPNProtocols)
	auxReq, _ := http.NewRequest(http.MethodGet, "https://example.com/probe", nil)
	auxResp, err := openAISvc.doOpenAIHTTPWithTLS(auxReq, "", openAIDefaultFPAcc)
	require.NoError(t, err)
	_ = auxResp.Body.Close()
	require.Equal(t, 2, openAIUpstream.doWithTLSCalls)

	// 8) routeAntigravityTest 在未指定 modelID 时按白名单或已观测 Gemini 模型精准分流
	geminiOnlyRelay := &Account{
		ID:       705,
		Platform: PlatformAntigravity,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			AntigravityModelRestrictionModeKey: "whitelist",
			"model_mapping": map[string]any{
				"gemini-2.5-flash": "gemini-2.5-flash",
			},
		},
	}
	require.Equal(t, "gemini-2.5-flash", resolveAntigravityRelayTestModel(geminiOnlyRelay, ""))

	// 9) OpenAI Images 流式 url 重写在无尾随空行 EOF 时自动补齐 SSE 事件终止空行
	imgFmt := "png"
	sseNoTrailingBlank := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("event: image_generation.completed\ndata: {\"type\":\"image_generation.completed\",\"b64_json\":\"YWJj\"}\n")),
	}
	recImg := httptest.NewRecorder()
	cImg, _ := gin.CreateTestContext(recImg)
	cImg.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	_, imgCount, _, _, imgErr := openAISvc.handleOpenAIImagesStreamingResponse(sseNoTrailingBlank, cImg, time.Now(), nil, &imgFmt)
	require.NoError(t, imgErr)
	require.Equal(t, 1, imgCount)
	require.Contains(t, recImg.Body.String(), `"url":"data:image/png;base64,YWJj"`)
	require.True(t, strings.HasSuffix(recImg.Body.String(), "\n\n"), "EOF flush must terminate buffered SSE event with blank line")
}

func BenchmarkPrepareGeminiRequestPayload_SinglePass(b *testing.B) {
	body := []byte(`{
		"contents":[
			{"role":"user","parts":[{"text":"Please inspect the repository and run tests."}]},
			{"role":"model","parts":[{"text":"Sure, checking files now."}]}
		],
		"generationConfig":{"thinkingConfig":{"thinkingBudget":4096}},
		"tools":[
			{"googleSearch":{}},
			{"functionDeclarations":[
				{"name":"read_file","parameters":{"type":"object","$schema":"https://json-schema.org/draft/2020-12/schema","properties":{"path":{"type":"string","minLength":1}},"required":["path"]}}
			]}
		]
	}`)
	acc := &Account{ID: 42, Platform: PlatformAntigravity, Type: AccountTypeOAuth}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reqMap, err := prepareGeminiRequestPayload(body, acc, "", false)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := marshalWrappedV1InternalRequest("proj-bench", "gemini-3.1-pro-high", reqMap); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGeminiThinkingLevelFromBody_FastPath(b *testing.B) {
	bodyWithThinking := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":4000}}}`)
	bodyPlain := []byte(`{"contents":[{"role":"user","parts":[{"text":"hello world without thinking config"}]}]}`)

	b.Run("with_thinking_config", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if got := geminiThinkingLevelFromBody(bodyWithThinking); got != "medium" {
				b.Fatalf("got %s", got)
			}
		}
	})
	b.Run("without_thinking_config", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if got := geminiThinkingLevelFromBody(bodyPlain); got != "high" {
				b.Fatalf("got %s", got)
			}
		}
	})
}
