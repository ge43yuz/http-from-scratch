package http1

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

func ListenAndServe(addr string, handler http.Handler) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	if handler == nil {
		handler = http.DefaultServeMux
	}

	for {
		accept, err := listener.Accept()
		if err != nil {
			fmt.Printf("Error accepting connection: %v\n", err)
			continue
		}

		go func() {
			err := HandleConnection(accept, handler)
			if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				fmt.Printf("Error handling connection: %v\n", err)
			}
		}()
	}
}

func HandleConnection(conn net.Conn, mux http.Handler) error {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	for {
		req, err := ParseRequest(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			statusCode := http.StatusBadRequest
			statusMsg := "Bad Request"

			switch {
			case errors.Is(err, ErrVersionNotSupported):
				statusCode = http.StatusHTTPVersionNotSupported
				statusMsg = "HTTP Version Not Supported"
			case errors.Is(err, ErrRequestURITooLong):
				statusCode = http.StatusRequestURITooLong
				statusMsg = "Request URI Too Long"
			case errors.Is(err, ErrHeaderFieldsTooLarge):
				statusCode = http.StatusRequestHeaderFieldsTooLarge
				statusMsg = "Request Header Fields Too Large"
			}

			sendErrorResponse(conn, statusCode, statusMsg)
			return nil
		}


		if strings.EqualFold(req.Header.Get("Expect"), "100-continue") {
			_, _ = conn.Write([]byte("HTTP/1.1 100 Continue\r\n\r\n"))
		}

		// keep-alive semantics
		keepAlive := true
		if req.ProtoMajor == 1 && req.ProtoMinor == 0 {
			keepAlive = strings.EqualFold(req.Header.Get("Connection"), "keep-alive")
		} else {
			if strings.EqualFold(req.Header.Get("Connection"), "close") || req.Close {
				keepAlive = false
			}
		}

		writer := NewResponse(conn, req.Method == http.MethodHead)

		if !keepAlive {
			writer.Header().Set("Connection", "close")
		} else if req.ProtoMajor == 1 && req.ProtoMinor == 0 {
			writer.Header().Set("Connection", "keep-alive")
		}

		mux.ServeHTTP(writer, req)

		if !writer.wroteHeader {
			if writer.Header().Get("Content-Length") == "" && req.Method != http.MethodHead {
				writer.Header().Set("Content-Length", "0")
			}
			writer.WriteHeader(http.StatusOK)
		}

		// Terminate chunked responses
		if writer.chunked && req.Method != http.MethodHead {
			_, _ = conn.Write([]byte("0\r\n\r\n"))
		}

		if strings.EqualFold(writer.Header().Get("Connection"), "close") {
			keepAlive = false
		}

		if !keepAlive {
			break
		}
	}
	return nil
}

func sendErrorResponse(conn net.Conn, code int, msg string) {
	reason := http.StatusText(code)
	if reason == "" {
		reason = msg
	}
	body := fmt.Sprintf("%d %s", code, reason)
	resp := fmt.Sprintf("HTTP/1.1 %d %s\r\nConnection: close\r\nContent-Type: text/plain\r\nContent-Length: %d\r\n\r\n%s",
		code, reason, len(body), body)
	_, _ = conn.Write([]byte(resp))
}
