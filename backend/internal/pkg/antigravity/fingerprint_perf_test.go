//go:build unit

package antigravity

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

func TestNewClientForAccount_IsolatesAccountsAndReusesWithinSameAccount(t *testing.T) {
	// 1) 同一账号（accountID > 0）重复调用复用专属连接池
	acct1A, err := NewClientForAccount("http://127.0.0.1:18080", 101, nil)
	if err != nil {
		t.Fatalf("NewClientForAccount acct1A failed: %v", err)
	}
	acct1B, err := NewClientForAccount("http://127.0.0.1:18080", 101, nil)
	if err != nil {
		t.Fatalf("NewClientForAccount acct1B failed: %v", err)
	}
	if acct1A.httpClient.Transport == nil || acct1A.httpClient.Transport != acct1B.httpClient.Transport {
		t.Fatal("expected same accountID + proxy to share its pooled RoundTripper")
	}

	// 2) 不同账号（即使共享同一动态旋转代理 URL）必须物理隔离 Transport，防止跨账号复用连接/SessionTicket
	acct2, err := NewClientForAccount("http://127.0.0.1:18080", 102, nil)
	if err != nil {
		t.Fatalf("NewClientForAccount acct2 failed: %v", err)
	}
	if acct1A.httpClient.Transport == acct2.httpClient.Transport {
		t.Fatal("expected distinct accounts on the same proxy URL to isolate RoundTrippers")
	}

	// 3) 匿名/未落库客户端（accountID <= 0）绝不进入全局共享池
	ephemeral1, err := NewClient("http://127.0.0.1:18080")
	if err != nil {
		t.Fatalf("NewClient ephemeral1 failed: %v", err)
	}
	ephemeral2, err := NewClient("http://127.0.0.1:18080")
	if err != nil {
		t.Fatalf("NewClient ephemeral2 failed: %v", err)
	}
	if ephemeral1.httpClient.Transport == ephemeral2.httpClient.Transport {
		t.Fatal("expected ephemeral clients (accountID=0) not to share pooled RoundTripper across calls")
	}

	// 4) 带 TLS 指纹模板时：同账号同线级模板复用，不同账号或模板变更时立即隔离
	profileA := tlsfingerprint.WithDefaultHTTP2ALPN(&tlsfingerprint.Profile{Name: "Profile-A", Curves: []uint16{29}, KeyShareGroups: []uint16{29}})
	profileB := tlsfingerprint.WithDefaultHTTP2ALPN(&tlsfingerprint.Profile{Name: "Profile-B", Curves: []uint16{29, 23}, KeyShareGroups: []uint16{29}})

	tlsAcct1A, err := NewClientForAccount("", 101, profileA)
	if err != nil {
		t.Fatalf("NewClientForAccount tlsAcct1A failed: %v", err)
	}
	tlsAcct1Renamed, err := NewClientForAccount("", 101, tlsfingerprint.WithDefaultHTTP2ALPN(&tlsfingerprint.Profile{Name: "Renamed-A", Curves: []uint16{29}, KeyShareGroups: []uint16{29}}))
	if err != nil {
		t.Fatalf("NewClientForAccount tlsAcct1Renamed failed: %v", err)
	}
	if tlsAcct1A.httpClient.Transport != tlsAcct1Renamed.httpClient.Transport {
		t.Fatal("expected same account with identical wire TLS profile to share pooled RoundTripper")
	}

	tlsAcct2SameProfile, err := NewClientForAccount("", 102, profileA)
	if err != nil {
		t.Fatalf("NewClientForAccount tlsAcct2SameProfile failed: %v", err)
	}
	if tlsAcct1A.httpClient.Transport == tlsAcct2SameProfile.httpClient.Transport {
		t.Fatal("expected distinct accounts with same TLS profile to isolate RoundTrippers and session caches")
	}

	tlsAcct1ProfileB, err := NewClientForAccount("", 101, profileB)
	if err != nil {
		t.Fatalf("NewClientForAccount tlsAcct1ProfileB failed: %v", err)
	}
	if tlsAcct1A.httpClient.Transport == tlsAcct1ProfileB.httpClient.Transport {
		t.Fatal("expected different wire TLS profiles on same account to isolate RoundTrippers")
	}

	// 不自洽的 TLS 模板在建池前立即拒绝
	invalidProfile := &tlsfingerprint.Profile{Name: "Invalid", Curves: []uint16{23}, KeyShareGroups: []uint16{29}}
	if _, err := NewClientForAccount("", 101, invalidProfile); err == nil {
		t.Fatal("expected inconsistent TLS profile to fail validation")
	}
}

func TestDecompressClientResponseBody_Gzip(t *testing.T) {
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, _ = gz.Write([]byte(`{"hello":"antigravity"}`))
	_ = gz.Close()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Encoding": []string{"gzip"}},
		Body:       io.NopCloser(bytes.NewReader(compressed.Bytes())),
	}
	httputil.DecompressResponseBody(resp)
	defer func() { _ = resp.Body.Close() }()

	decoded, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read decompressed body: %v", err)
	}
	if string(decoded) != `{"hello":"antigravity"}` {
		t.Fatalf("unexpected decoded payload: %s", string(decoded))
	}
	if resp.Header.Get("Content-Encoding") != "" {
		t.Fatal("expected Content-Encoding header to be stripped after decompression")
	}
}

func TestClient_AllEndpointsCarryConsistentDeviceIdentity(t *testing.T) {
	var seenLoadUA, seenLoadClientMeta, seenLoadPlatform string
	var seenOnboardUA, seenOnboardClientMeta, seenOnboardPlatform string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			seenLoadUA = r.Header.Get("User-Agent")
			seenLoadClientMeta = r.Header.Get("Client-Metadata")
			var req LoadCodeAssistRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			seenLoadPlatform = req.Metadata.Platform
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cloudaicompanionProject":"proj-1"}`))
		case "/v1internal:onboardUser":
			seenOnboardUA = r.Header.Get("User-Agent")
			seenOnboardClientMeta = r.Header.Get("Client-Metadata")
			var req OnboardUserRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			seenOnboardPlatform = req.Metadata.Platform
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"done":true,"response":{"cloudaicompanionProject":"proj-1"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	withMockBaseURLs(t, []string{server.URL})

	client, err := NewClientWithTLS("", &tlsfingerprint.Profile{Name: "Test-TLS", Curves: []uint16{29}, KeyShareGroups: []uint16{29}})
	if err != nil {
		t.Fatalf("NewClientWithTLS: %v", err)
	}

	// 1) 未钉设备时：UA (windows/amd64)、Client-Metadata (WINDOWS)、请求体 Platform (WINDOWS_AMD64) 三端自洽
	if _, _, err := client.LoadCodeAssist(context.Background(), "tok"); err != nil {
		t.Fatalf("LoadCodeAssist default: %v", err)
	}
	if !strings.HasSuffix(seenLoadUA, " windows/amd64") || !strings.Contains(seenLoadClientMeta, `"platform":"WINDOWS"`) || seenLoadPlatform != "WINDOWS_AMD64" {
		t.Fatalf("default identity mismatch: ua=%q meta=%q bodyPlatform=%q", seenLoadUA, seenLoadClientMeta, seenLoadPlatform)
	}

	if _, err := client.OnboardUser(context.Background(), "tok", "free-tier"); err != nil {
		t.Fatalf("OnboardUser default: %v", err)
	}
	if seenOnboardPlatform != "WINDOWS_AMD64" {
		t.Fatalf("expected default OnboardUser platform WINDOWS_AMD64, got %q", seenOnboardPlatform)
	}

	// 2) 钉住 darwin/arm64 时：三端同步切换为 darwin/arm64 + MACOS + DARWIN_ARM64
	darwinCtx := WithDeviceIdentity(context.Background(), "darwin", "arm64", "DARWIN_ARM64")
	if _, _, err := client.LoadCodeAssist(darwinCtx, "tok"); err != nil {
		t.Fatalf("LoadCodeAssist darwin: %v", err)
	}
	if !strings.HasSuffix(seenLoadUA, " darwin/arm64") || !strings.Contains(seenLoadClientMeta, `"platform":"MACOS"`) || seenLoadPlatform != "DARWIN_ARM64" {
		t.Fatalf("darwin identity mismatch: ua=%q meta=%q bodyPlatform=%q", seenLoadUA, seenLoadClientMeta, seenLoadPlatform)
	}

	if _, err := client.OnboardUser(darwinCtx, "tok", "free-tier"); err != nil {
		t.Fatalf("OnboardUser darwin: %v", err)
	}
	if !strings.HasSuffix(seenOnboardUA, " darwin/arm64") || !strings.Contains(seenOnboardClientMeta, `"platform":"MACOS"`) || seenOnboardPlatform != "DARWIN_ARM64" {
		t.Fatalf("darwin onboard mismatch: ua=%q meta=%q bodyPlatform=%q", seenOnboardUA, seenOnboardClientMeta, seenOnboardPlatform)
	}
}

func TestDeriveAntigravitySessionID_StripsClaudeCodeFingerprintAndIsolatesAccounts(t *testing.T) {
	claudeLegacyUID := "user_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef_account_11111111-2222-3333-4444-555555555555_session_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	claudeJSONUID := `{"device_id":"0123456789abcdef","account_uuid":"11111111-2222-3333-4444-555555555555","session_id":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}`

	// 提取出的会话线索一致且不含 device_id / account_uuid
	if got := ExtractClientSessionHint(claudeLegacyUID); got != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("unexpected legacy session hint: %q", got)
	}
	if got := ExtractClientSessionHint(claudeJSONUID); got != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("unexpected json session hint: %q", got)
	}

	fixedNow := time.Unix(1_700_000_100, 0)
	sidAcct1A := deriveAntigravitySessionIDAt(claudeLegacyUID, "hello", "ag-acct-1", false, true, fixedNow)
	sidAcct1B := deriveAntigravitySessionIDAt(claudeJSONUID, "second turn", "ag-acct-1", false, true, fixedNow)
	sidAcct2 := deriveAntigravitySessionIDAt(claudeLegacyUID, "hello", "ag-acct-2", false, true, fixedNow)

	if !isCanonicalAntigravitySessionID(sidAcct1A) {
		t.Fatalf("expected canonical -<digits> sessionId, got %q", sidAcct1A)
	}
	if strings.Contains(sidAcct1A, "user_") || strings.Contains(sidAcct1A, "account_") {
		t.Fatalf("claude fingerprint leaked into sessionId: %q", sidAcct1A)
	}
	if sidAcct1A != sidAcct1B {
		t.Fatalf("expected same client session on same account to stay stable across turns: %q vs %q", sidAcct1A, sidAcct1B)
	}
	if sidAcct1A == sidAcct2 {
		t.Fatalf("expected failover across accounts to isolate sessionId, both got %q", sidAcct1A)
	}

	// 开启 MaskSessionID 后，不同客户端在同一 15m 桶内收敛到同一个账号级 sessionId
	maskedClient1 := deriveAntigravitySessionIDAt("session-client-1", "hi", "ag-acct-1", true, true, fixedNow)
	maskedClient2 := deriveAntigravitySessionIDAt("session-client-2", "other", "ag-acct-1", true, true, fixedNow.Add(5*time.Minute))
	if maskedClient1 != maskedClient2 {
		t.Fatalf("expected session masking to converge distinct clients within window: %q vs %q", maskedClient1, maskedClient2)
	}
}

func BenchmarkCleanJSONSchema_ComplexToolSet(b *testing.B) {
	rawJSON := []byte(`{
		"$defs": {
			"Step": {
				"type": "object",
				"additionalProperties": false,
				"properties": {
					"title": {"type": "string", "minLength": 1},
					"status": {"const": "pending"}
				},
				"required": ["title", "status", "nonexistent"]
			}
		},
		"type": ["object", "null"],
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"properties": {
			"query": {"type": "string", "pattern": "^[a-z]+$"},
			"steps": {"type": "array", "prefixItems": [{"$ref": "#/$defs/Step"}]}
		},
		"required": ["query"]
	}`)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var schema map[string]any
		_ = json.Unmarshal(rawJSON, &schema)
		_ = CleanJSONSchema(schema)
	}
}

func BenchmarkStreamingProcessor_FormatEvents(b *testing.B) {
	const line = `data: {"responseId":"msg_1","candidates":[{"content":{"parts":[{"text":"hello world "}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":5}}`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := NewStreamingProcessor("claude-sonnet-4-6")
		_ = p.ProcessLine(line)
		_, _ = p.Finish()
	}
}
