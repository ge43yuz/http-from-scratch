package http1

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

var (
	ErrBadRequest           = errors.New("bad request")
	ErrVersionNotSupported  = errors.New("http version not supported")
	ErrRequestURITooLong    = errors.New("request uri too long")
	ErrHeaderFieldsTooLarge = errors.New("request header fields too large")
	ErrLineTooLong          = errors.New("line too long")
)

const (
	maxRequestLineLen = 8192
	maxHeaderLineLen  = 8192
	maxHeaderCount    = 1000
)

func ParseRequest(reader *bufio.Reader) (*http.Request, error) {
	rl, err := readCRLFLine(reader, maxRequestLineLen)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		if errors.Is(err, ErrLineTooLong) {
			return nil, ErrRequestURITooLong
		}
		return nil, ErrBadRequest
	}

	method, rawURL, proto, major, minor, err := parseRequestLine(rl)
	if err != nil {
		return nil, err
	}

	headers := make(http.Header)
	headerCount := 0
	var rawCLValues []string

	for {
		line, err := readCRLFLine(reader, maxHeaderLineLen)
		if err != nil {
			if errors.Is(err, ErrLineTooLong) {
				return nil, ErrHeaderFieldsTooLarge
			}
			return nil, ErrBadRequest
		}
		if line == "" {
			break
		}

		headerCount++
		if headerCount > maxHeaderCount {
			return nil, ErrHeaderFieldsTooLarge
		}

		key, val, err := parseHeaderLine(line)
		if err != nil {
			return nil, err
		}

		if strings.EqualFold(key, "Content-Length") {
			rawCLValues = append(rawCLValues, val)
		}

		headers.Add(key, val)
	}

	var contentLength int64 = -1
	if len(rawCLValues) > 0 {
		var clVal int64 = -1
		for _, rawVal := range rawCLValues {
			parts := strings.SplitSeq(rawVal, ",")
			for part := range parts {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				v, err := strconv.ParseInt(part, 10, 64)
				if err != nil || v < 0 {
					return nil, ErrBadRequest
				}
				if clVal == -1 {
					clVal = v
				} else if clVal != v {
					return nil, ErrBadRequest
				}
			}
		}
		contentLength = clVal
	}

	var bodyBytes []byte
	teHeader := headers.Get("Transfer-Encoding")

	if strings.Contains(strings.ToLower(teHeader), "chunked") {
		b, err := readChunkedBody(reader)
		if err != nil {
			return nil, err
		}
		bodyBytes = b
		contentLength = -1
	} else if contentLength >= 0 {
		b, err := readFixedBody(reader, contentLength)
		if err != nil {
			return nil, err
		}
		bodyBytes = b
	} else {
		bodyBytes = []byte{}
		contentLength = 0
	}

	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return nil, ErrBadRequest
	}

	host := parsedURL.Host
	if host == "" {
		host = headers.Get("Host")
	}

	shouldClose := strings.EqualFold(headers.Get("Connection"), "close")

	req := &http.Request{
		Method:        method,
		URL:           parsedURL,
		Proto:         proto,
		ProtoMajor:    major,
		ProtoMinor:    minor,
		Header:        headers,
		Host:          host,
		RequestURI:    rawURL,
		Body:          io.NopCloser(bytes.NewReader(bodyBytes)),
		ContentLength: contentLength,
		Close:         shouldClose,
	}

	if teHeader != "" {
		req.TransferEncoding = []string{"chunked"}
	}

	return req, nil
}

func parseRequestLine(rl string) (string, string, string, int, int, error) {
	parts := strings.Split(rl, " ")
	if len(parts) != 3 {
		return "", "", "", 0, 0, ErrBadRequest
	}
	method, rawURL, proto := parts[0], parts[1], parts[2]

	if !isValidMethod(method) || rawURL == "" {
		return "", "", "", 0, 0, ErrBadRequest
	}

	major, minor, err := parseVersion(proto)
	if err != nil {
		return "", "", "", 0, 0, err
	}

	return method, rawURL, proto, major, minor, nil
}

func isValidMethod(method string) bool {
	if len(method) == 0 {
		return false
	}
	for i := 0; i < len(method); i++ {
		if !isTchar(method[i]) {
			return false
		}
	}
	return true
}

func isTchar(b byte) bool {
	if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') {
		return true
	}
	switch b {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

func parseVersion(proto string) (int, int, error) {
	if !strings.HasPrefix(proto, "HTTP/") {
		return 0, 0, ErrBadRequest
	}
	verStr := strings.TrimPrefix(proto, "HTTP/")
	verParts := strings.Split(verStr, ".")
	if len(verParts) != 2 {
		return 0, 0, ErrBadRequest
	}
	if !isAllDigits(verParts[0]) || !isAllDigits(verParts[1]) {
		return 0, 0, ErrBadRequest
	}
	major, err1 := strconv.Atoi(verParts[0])
	minor, err2 := strconv.Atoi(verParts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, ErrBadRequest
	}

	if (major == 1 && minor == 1) || (major == 1 && minor == 0) {
		return major, minor, nil
	}

	return major, minor, ErrVersionNotSupported
}

func isAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func parseHeaderLine(line string) (string, string, error) {
	key, val, found := strings.Cut(line, ":")
	if !found || len(key) == 0 {
		return "", "", ErrBadRequest
	}

	for i := 0; i < len(key); i++ {
		if !isValidHeaderNameByte(key[i]) {
			return "", "", ErrBadRequest
		}
	}

	for i := 0; i < len(val); i++ {
		b := val[i]
		if b == '\t' || b == '\r' || b == '\n' || b < 32 || b == 127 {
			return "", "", ErrBadRequest
		}
	}

	return strings.TrimSpace(key), strings.TrimSpace(val), nil
}

func isValidHeaderNameByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-' || b == '_'
}

func readFixedBody(reader *bufio.Reader, size int64) ([]byte, error) {
	if size == 0 {
		return []byte{}, nil
	}
	body := make([]byte, size)
	_, err := io.ReadFull(reader, body)
	if err != nil {
		return nil, ErrBadRequest
	}
	return body, nil
}

func readChunkedBody(reader *bufio.Reader) ([]byte, error) {
	var body []byte
	for {
		line, err := readCRLFLine(reader, maxHeaderLineLen)
		if err != nil {
			return nil, ErrBadRequest
		}

		chunkHead, _, _ := strings.Cut(line, ";")
		chunkHead = strings.TrimSpace(chunkHead)
		if len(chunkHead) == 0 {
			return nil, ErrBadRequest
		}

		size, err := strconv.ParseUint(chunkHead, 16, 64)
		if err != nil {
			return nil, ErrBadRequest
		}

		if size == 0 {
			for {
				tline, err := readCRLFLine(reader, maxHeaderLineLen)
				if err != nil {
					return nil, ErrBadRequest
				}
				if tline == "" {
					break
				}
				_, _, err = parseHeaderLine(tline)
				if err != nil {
					return nil, ErrBadRequest
				}
			}
			return body, nil
		}

		chunkData := make([]byte, size)
		_, err = io.ReadFull(reader, chunkData)
		if err != nil {
			return nil, ErrBadRequest
		}
		body = append(body, chunkData...)

		crlf := make([]byte, 2)
		_, err = io.ReadFull(reader, crlf)
		if err != nil || crlf[0] != '\r' || crlf[1] != '\n' {
			return nil, ErrBadRequest
		}
	}
}

func readCRLFLine(reader *bufio.Reader, maxLen int) (string, error) {
	var lineBytes []byte
	for {
		b, err := reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && len(lineBytes) == 0 {
				return "", io.EOF
			}
			return "", ErrBadRequest
		}
		lineBytes = append(lineBytes, b)
		if maxLen > 0 && len(lineBytes) > maxLen {
			return "", ErrLineTooLong
		}
		if b == '\n' {
			break
		}
	}
	n := len(lineBytes)
	if n < 2 || lineBytes[n-2] != '\r' || lineBytes[n-1] != '\n' {
		return "", ErrBadRequest
	}
	body := lineBytes[:n-2]
	if slices.Contains(body, '\r') {
		return "", ErrBadRequest
	}
	return string(body), nil
}
