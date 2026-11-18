//go:build unit

package service

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The official Images API answers response_format on GPT Image models with
// 400 "Unknown parameter"; relays keep it because some honour it.
func TestForwardOpenAIImagesAPIKey_ResponseFormat(t *testing.T) {
	relay := newOpenAIImagesAPIKeyAccount()
	relay.Credentials["base_url"] = "https://relay.example.test/v1"
	noBaseURL := newOpenAIImagesAPIKeyAccount()
	delete(noBaseURL.Credentials, "base_url")

	for _, tc := range []struct {
		name    string
		account *Account
		body    string
		dropped bool
		wantURL bool
	}{
		{name: "official b64_json", account: newOpenAIImagesAPIKeyAccount(), body: `{"model":"gpt-image-2.6-flare","prompt":"apple","response_format":"b64_json"}`, dropped: true},
		{name: "official url becomes data URL", account: newOpenAIImagesAPIKeyAccount(), body: `{"model":"gpt-image-2.6-flare","prompt":"apple","response_format":"url","output_format":"webp"}`, dropped: true, wantURL: true},
		{name: "default base URL is official", account: noBaseURL, body: `{"model":"gpt-image-2","prompt":"apple","response_format":"url"}`, dropped: true, wantURL: true},
		{name: "relay keeps response_format", account: relay, body: `{"model":"gpt-image-2.6-flare","prompt":"apple","response_format":"url"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.body)
			c, rec := newOpenAIImagesTestContext(t, body)
			upstream := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
			svc := newOpenAIImagesTestService(upstream)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)

			result, err := svc.ForwardImages(context.Background(), c, tc.account, body, parsed, "")
			require.NoError(t, err)
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, !tc.dropped, gjson.GetBytes(upstream.lastBody, "response_format").Exists(), string(upstream.lastBody))
			require.Equal(t, "apple", gjson.GetBytes(upstream.lastBody, "prompt").String())

			out := rec.Body.Bytes()
			if tc.wantURL {
				format := gjson.GetBytes(body, "output_format").String()
				mime := "image/png"
				if format == "webp" {
					mime = "image/webp"
				}
				require.Equal(t, "data:"+mime+";base64,aGVsbG8=", gjson.GetBytes(out, "data.0.url").String())
				require.False(t, gjson.GetBytes(out, "data.0.b64_json").Exists())
			} else {
				require.Equal(t, "aGVsbG8=", gjson.GetBytes(out, "data.0.b64_json").String())
			}
		})
	}
}

func TestForwardOpenAIImagesAPIKey_OfficialMultipartDropsResponseFormat(t *testing.T) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, value := range map[string]string{"model": "gpt-image-2.6-sunburst", "prompt": "make it blue", "response_format": "b64_json", "quality": "max"} {
		require.NoError(t, writer.WriteField(key, value))
	}
	part, err := writer.CreateFormFile("image", "ref.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("\x89PNG\r\n\x1a\nfake"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	body := buf.Bytes()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	upstream := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
	svc := newOpenAIImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	_, err = svc.ForwardImages(context.Background(), c, newOpenAIImagesAPIKeyAccount(), body, parsed, "")
	require.NoError(t, err)

	fields, files := parseStubMultipart(upstream.lastReq.Header.Get("Content-Type"), upstream.lastBody)
	require.Equal(t, 1, files)
	require.NotContains(t, fields, "response_format")
	require.Equal(t, "max", fields["quality"])
	require.Equal(t, "make it blue", fields["prompt"])
	require.Equal(t, "aGVsbG8=", gjson.GetBytes(rec.Body.Bytes(), "data.0.b64_json").String())
}
