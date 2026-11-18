package httputil

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// DecompressResponseBody 根据 Content-Encoding 解压响应体。
// 当请求显式设置了 accept-encoding 时，Go 的 Transport 不会自动解压，需要手动处理。
// 解压成功后会删除 Content-Encoding 和 Content-Length header（长度已不准确）。
func DecompressResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	ce := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	if ce == "" {
		return
	}

	originalBody := resp.Body
	var reader io.Reader
	switch ce {
	case "gzip":
		br := bufio.NewReader(originalBody)
		resp.Body = &decompressedBody{reader: br, closer: originalBody}
		gr, err := gzip.NewReader(br)
		if err != nil {
			return // 解压失败，保持原样
		}
		reader = gr
	case "br":
		reader = brotli.NewReader(resp.Body)
	case "deflate":
		br := bufio.NewReader(originalBody)
		header, _ := br.Peek(2)
		// RFC 9110 specifies a zlib wrapper. Some relays still send raw DEFLATE.
		if len(header) == 2 && header[0]&15 == 8 && header[0]>>4 <= 7 && binary.BigEndian.Uint16(header)%31 == 0 {
			resp.Body = &decompressedBody{reader: br, closer: originalBody}
			if header[1]&0x20 != 0 {
				return // A preset dictionary is unavailable; preserve the encoded body.
			}
			zr, err := zlib.NewReader(br)
			if err != nil {
				return
			}
			reader = zr
		} else {
			reader = flate.NewReader(br)
		}
	case "zstd":
		bufferedBody := bufio.NewReader(resp.Body)
		resp.Body = &decompressedBody{reader: bufferedBody, closer: originalBody}

		headerBytes, _ := bufferedBody.Peek(zstd.HeaderMaxSize)
		var header zstd.Header
		if err := header.Decode(headerBytes); err != nil {
			slog.Warn("zstd_decompress_failed", "error", err)
			return
		}

		zr, err := zstd.NewReader(bufferedBody)
		if err != nil {
			slog.Warn("zstd_decompress_failed", "error", err)
			return
		}
		reader = &zstdResponseReader{ReadCloser: zr.IOReadCloser()}
	default:
		return
	}

	resp.Body = &decompressedBody{reader: reader, closer: originalBody}
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length") // 解压后长度不确定
	resp.ContentLength = -1
}

type zstdResponseReader struct {
	io.ReadCloser
	warnOnce sync.Once
}

func (r *zstdResponseReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		r.warnOnce.Do(func() {
			slog.Warn("zstd_decompress_failed", "error", err)
		})
	}
	return n, err
}

// decompressedBody 组合解压 reader 和原始 body 的 close。
type decompressedBody struct {
	reader io.Reader
	closer io.Closer
}

func (d *decompressedBody) Read(p []byte) (int, error) {
	return d.reader.Read(p)
}

func (d *decompressedBody) Close() error {
	// 如果 reader 本身也是 Closer（如 gzip.Reader），先关闭它
	if rc, ok := d.reader.(io.Closer); ok {
		_ = rc.Close()
	}
	return d.closer.Close()
}
