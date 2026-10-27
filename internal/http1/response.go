package http1

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

type Response struct {
	conn        net.Conn
	header      http.Header
	wroteHeader bool
	statusCode  int
	chunked     bool
	isHead      bool
}

func NewResponse(conn net.Conn, isHead bool) *Response {
	return &Response{
		conn:       conn,
		header:     make(http.Header),
		statusCode: http.StatusOK,
		isHead:     isHead,
	}
}

func (r *Response) Header() http.Header {
	return r.header
}

func (r *Response) WriteHeader(statusCode int) {
	if r.wroteHeader {
		return
	}
	r.statusCode = statusCode
	r.wroteHeader = true

	isClose := strings.EqualFold(r.header.Get("Connection"), "close")
	hasCL := r.header.Get("Content-Length") != ""
	hasTE := r.header.Get("Transfer-Encoding") != ""

	if !isClose && !hasCL && !hasTE {
		r.chunked = true
		r.header.Set("Transfer-Encoding", "chunked")
	}

	reasonPhrase := http.StatusText(statusCode)
	if reasonPhrase == "" {
		reasonPhrase = "Unknown"
	}
	statusLine := fmt.Sprintf("HTTP/1.1 %d %s\r\n", statusCode, reasonPhrase)
	_, _ = r.conn.Write([]byte(statusLine))

	_ = r.header.Write(r.conn)
	_, _ = r.conn.Write([]byte("\r\n"))
}

func (r *Response) Write(data []byte) (n int, err error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	
	if r.isHead {
		return len(data), nil
	}
	if r.chunked {
		if len(data) > 0 {
			_, err := fmt.Fprintf(r.conn, "%x\r\n", len(data))
			if err != nil {
				return 0, err
			}
			_, err = r.conn.Write(data)
			if err != nil {
				return 0, err
			}
			_, err = r.conn.Write([]byte("\r\n"))
			if err != nil {
				return 0, err
			}
		}
		return len(data), nil
	}
	return r.conn.Write(data)
}
