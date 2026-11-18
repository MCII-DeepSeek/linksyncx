//go:build unit

package tlsfingerprint

import (
	"bytes"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

func TestHTTP2CapableTransport_SpeaksNativeH2AndFallsBackToH1WithBodyIntact(t *testing.T) {
	pool, cert := newTestPKI(t)

	// 1) 启动真实支持 h2 ALPN 的 HTTPS 服务器
	h2Ln := listenLocal(t)
	h2Server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("X-Proto", r.Proto)
			_, _ = w.Write(body)
		}),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h2", "http/1.1"},
		},
	}
	_ = http2.ConfigureServer(h2Server, &http2.Server{})
	go func() {
		_ = h2Server.Serve(tls.NewListener(h2Ln, h2Server.TLSConfig))
	}()
	t.Cleanup(func() { _ = h2Server.Close() })

	// 2) 启动仅支持 http/1.1 ALPN 的 HTTPS 服务器
	h1Ln := listenLocal(t)
	h1Server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("X-Proto", r.Proto)
			_, _ = w.Write(body)
		}),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"http/1.1"},
		},
	}
	go func() {
		_ = h1Server.Serve(tls.NewListener(h1Ln, h1Server.TLSConfig))
	}()
	t.Cleanup(func() { _ = h1Server.Close() })

	profile := WithDefaultHTTP2ALPN(&Profile{
		Name:           "Antigravity-H2",
		Curves:         []uint16{29},
		KeyShareGroups: []uint16{29},
	})
	require.True(t, ProfileSupportsHTTP2(profile))

	rt, err := NewHTTP2CapableTransport(profile, nil, DialOptions{RootCAs: pool}, HTTP2TransportConfig{
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     30 * time.Second,
		ReadIdleTimeout:     10 * time.Second,
		PingTimeout:         5 * time.Second,
	})
	require.NoError(t, err)
	client := &http.Client{Transport: rt, Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)

	// 验证目标支持 h2 时走真实 HTTP/2.0
	respH2, err := client.Post("https://"+h2Ln.Addr().String()+"/echo", "application/json", bytes.NewReader([]byte(`{"h2":true}`)))
	require.NoError(t, err)
	gotH2, _ := io.ReadAll(respH2.Body)
	_ = respH2.Body.Close()
	require.Equal(t, "HTTP/2.0", respH2.Header.Get("X-Proto"))
	require.Equal(t, `{"h2":true}`, string(gotH2))

	// 验证目标仅支持 http/1.1 时自动回退到 HTTP/1.1 且 POST 请求体完整无损
	respH1, err := client.Post("https://"+h1Ln.Addr().String()+"/echo", "application/json", bytes.NewReader([]byte(`{"fallback":"h1"}`)))
	require.NoError(t, err)
	gotH1, _ := io.ReadAll(respH1.Body)
	_ = respH1.Body.Close()
	require.Equal(t, "HTTP/1.1", respH1.Header.Get("X-Proto"))
	require.Equal(t, `{"fallback":"h1"}`, string(gotH1))
}

func TestHTTP2CapableTransport_ResponseHeaderTimeoutAndLongStream(t *testing.T) {
	pool, cert := newTestPKI(t)
	releaseHang := make(chan struct{})
	t.Cleanup(func() { close(releaseHang) })

	h2Ln := listenLocal(t)
	h2Server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/hang-headers":
				select {
				case <-releaseHang:
				case <-r.Context().Done():
				}
			case "/long-stream":
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				for i := 0; i < 3; i++ {
					time.Sleep(50 * time.Millisecond)
					_, _ = w.Write([]byte("chunk;"))
					if f, ok := w.(http.Flusher); ok {
						f.Flush()
					}
				}
			}
		}),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h2"},
		},
	}
	_ = http2.ConfigureServer(h2Server, &http2.Server{})
	go func() {
		_ = h2Server.Serve(tls.NewListener(h2Ln, h2Server.TLSConfig))
	}()
	t.Cleanup(func() { _ = h2Server.Close() })

	profile := WithDefaultHTTP2ALPN(&Profile{
		Name:           "Antigravity-H2-Timeout",
		Curves:         []uint16{29},
		KeyShareGroups: []uint16{29},
	})
	rt, err := NewHTTP2CapableTransport(profile, nil, DialOptions{RootCAs: pool}, HTTP2TransportConfig{
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 80 * time.Millisecond,
		ReadIdleTimeout:       10 * time.Second,
		PingTimeout:           5 * time.Second,
	})
	require.NoError(t, err)
	// 数据面流式客户端无总 Timeout，完全依赖 ResponseHeaderTimeout 防挂死
	client := &http.Client{Transport: rt}
	t.Cleanup(client.CloseIdleConnections)

	// 1) 上游迟迟不回响应头时，必须按 ResponseHeaderTimeout 快速超时返回
	start := time.Now()
	resp, err := client.Get("https://" + h2Ln.Addr().String() + "/hang-headers")
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	require.Less(t, time.Since(start), 2*time.Second)
	require.ErrorContains(t, err, "timeout awaiting response headers")
	var netErr net.Error
	require.ErrorAs(t, err, &netErr)
	require.True(t, netErr.Timeout())

	// 2) 响应头在超时窗口内到达后，后续总时长超过 2x ResponseHeaderTimeout 的 SSE 流不能被误中断
	streamResp, err := client.Get("https://" + h2Ln.Addr().String() + "/long-stream")
	require.NoError(t, err)
	streamBytes, err := io.ReadAll(streamResp.Body)
	_ = streamResp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, "chunk;chunk;chunk;", string(streamBytes))

	// 3) HTTPS 默认端口 :443 归一化一致
	reqImplicit, _ := http.NewRequest(http.MethodGet, "https://Example.COM/v1", nil)
	reqExplicit, _ := http.NewRequest(http.MethodGet, "https://example.com:443/v1", nil)
	require.Equal(t, "example.com", canonicalHTTPSHostKey(reqImplicit))
	require.Equal(t, "example.com", canonicalHTTPSHostKey(reqExplicit))
}
