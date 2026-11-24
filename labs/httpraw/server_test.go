package httpraw_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gulmix/orders/labs/httpraw"
)

// startTestServer запускает сервер на 127.0.0.1:0 и возвращает его адрес и функцию остановки.
func startTestServer(t *testing.T, srv *httpraw.Server) (string, func()) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- srv.Serve(ctx, ln)
	}()

	cleanup := func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("Serve returned unexpected error on shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("timeout waiting for Serve to stop")
		}
	}

	return ln.Addr().String(), cleanup
}

// 1. GET и POST с телом — хендлер видит метод, цель, заголовки и тело.
func TestServer_GetAndPost(t *testing.T) {
	var (
		lastMethod string
		lastTarget string
		lastHeader map[string]string
		lastBody   []byte
	)

	srv := &httpraw.Server{
		Handler: func(req *httpraw.Request) *httpraw.Response {
			lastMethod = req.Method
			lastTarget = req.Target
			lastHeader = req.Header
			lastBody = req.Body

			switch req.Method {
			case "GET":
				return &httpraw.Response{
					Status: 200,
					Header: map[string]string{"Content-Type": "application/json"},
					Body:   []byte(`{"status":"ok"}`),
				}
			case "POST":
				return &httpraw.Response{
					Status: 201,
					Header: map[string]string{"Location": "/orders/123"},
					Body:   []byte(`created: 123`),
				}
			default:
				return &httpraw.Response{Status: 405}
			}
		},
	}

	addr, stop := startTestServer(t, srv)
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("GET", func(t *testing.T) {
		req, err := http.NewRequestWithContext(context.Background(), "GET", "http://"+addr+"/orders/42?active=true", nil)
		if err != nil {
			t.Fatalf("NewRequest error: %v", err)
		}
		req.Header.Set("X-Custom-Header", "test-val")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != `{"status":"ok"}` {
			t.Errorf("unexpected response body: %q", string(body))
		}

		if lastMethod != "GET" {
			t.Errorf("expected method GET, got %q", lastMethod)
		}
		if lastTarget != "/orders/42?active=true" {
			t.Errorf("expected target /orders/42?active=true, got %q", lastTarget)
		}
		if lastHeader["x-custom-header"] != "test-val" {
			t.Errorf("expected header x-custom-header 'test-val', got %q", lastHeader["x-custom-header"])
		}
		if len(lastBody) != 0 {
			t.Errorf("expected empty body for GET, got %d bytes", len(lastBody))
		}
	})

	t.Run("POST with body", func(t *testing.T) {
		reqBody := `{"sku":"item-99","qty":2}`
		req, err := http.NewRequestWithContext(context.Background(), "POST", "http://"+addr+"/orders", strings.NewReader(reqBody))
		if err != nil {
			t.Fatalf("NewRequest error: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("expected status 201, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != `created: 123` {
			t.Errorf("unexpected response body: %q", string(body))
		}

		if lastMethod != "POST" {
			t.Errorf("expected method POST, got %q", lastMethod)
		}
		if lastTarget != "/orders" {
			t.Errorf("expected target /orders, got %q", lastTarget)
		}
		if string(lastBody) != reqBody {
			t.Errorf("expected body %q, got %q", reqBody, string(lastBody))
		}
	})
}

// 2. Keep-alive. Два запроса подряд идут через одно соединение.
// Проверяйте через httptrace.GotConnInfo.Reused.
func TestServer_KeepAlive(t *testing.T) {
	var reqCount atomic.Int32

	srv := &httpraw.Server{
		Handler: func(req *httpraw.Request) *httpraw.Response {
			reqCount.Add(1)
			return &httpraw.Response{
				Status: 200,
				Body:   []byte(fmt.Sprintf("response #%d", reqCount.Load())),
			}
		},
	}

	addr, stop := startTestServer(t, srv)
	defer stop()

	transport := &http.Transport{
		DisableKeepAlives: false,
		MaxIdleConns:      10,
		IdleConnTimeout:   5 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}

	var firstReused, secondReused bool

	// Первый запрос
	trace1 := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			firstReused = info.Reused
		},
	}
	req1, _ := http.NewRequestWithContext(
		httptrace.WithClientTrace(context.Background(), trace1),
		"GET",
		"http://"+addr+"/ping1",
		nil,
	)
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp1.Body)
	resp1.Body.Close()

	if firstReused {
		t.Errorf("first request should NOT have reused connection")
	}

	// Второй запрос к тому же серверу
	trace2 := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			secondReused = info.Reused
		},
	}
	req2, _ := http.NewRequestWithContext(
		httptrace.WithClientTrace(context.Background(), trace2),
		"GET",
		"http://"+addr+"/ping2",
		nil,
	)
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()

	if !secondReused {
		t.Errorf("second request MUST reuse the connection (httptrace.GotConnInfo.Reused == true)")
	}
}

// 3. Chunked в обе стороны: запрос с телом неизвестной длины и ответ с Chunks.
func TestServer_ChunkedBothDirections(t *testing.T) {
	var receivedBody []byte
	var receivedTE string

	srv := &httpraw.Server{
		Handler: func(req *httpraw.Request) *httpraw.Response {
			receivedBody = req.Body
			receivedTE = req.Header["transfer-encoding"]

			return &httpraw.Response{
				Status: 200,
				Chunks: [][]byte{
					[]byte("chunk-one:"),
					[]byte("chunk-two:"),
					[]byte("chunk-three"),
				},
			}
		},
	}

	addr, stop := startTestServer(t, srv)
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}

	// Отправляем тело через io.Reader с неизвестной длиной (ContentLength = -1)
	// Стандартный net/http отправит его с Transfer-Encoding: chunked
	pipeR, pipeW := io.Pipe()
	go func() {
		defer pipeW.Close()
		_, _ = pipeW.Write([]byte("streaming-data-part-1;"))
		time.Sleep(10 * time.Millisecond)
		_, _ = pipeW.Write([]byte("streaming-data-part-2"))
	}()

	req, err := http.NewRequestWithContext(context.Background(), "POST", "http://"+addr+"/chunked-test", pipeR)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	req.ContentLength = -1 // принудительный chunked в net/http

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST chunked request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Проверяем, что ответ пришел chunked
	if len(resp.TransferEncoding) == 0 || resp.TransferEncoding[0] != "chunked" {
		t.Errorf("expected chunked Transfer-Encoding in response, got: %v", resp.TransferEncoding)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	expectedResp := "chunk-one:chunk-two:chunk-three"
	if string(respBody) != expectedResp {
		t.Errorf("expected response %q, got %q", expectedResp, string(respBody))
	}

	// Проверяем, что сервер принял и раскодировал chunked-запрос
	if !strings.Contains(receivedTE, "chunked") {
		t.Errorf("expected server to see chunked transfer-encoding, got %q", receivedTE)
	}
	expectedReqBody := "streaming-data-part-1;streaming-data-part-2"
	if string(receivedBody) != expectedReqBody {
		t.Errorf("expected server to receive decoded body %q, got %q", expectedReqBody, string(receivedBody))
	}
}

// 4. Плохие запросы через net.Dial и сырые байты:
// кривая стартовая строка, оба заголовка длины, слишком длинные заголовки, неверная версия — проверяйте коды.
func TestServer_BadRequestsRawBytes(t *testing.T) {
	srv := &httpraw.Server{
		Handler: func(req *httpraw.Request) *httpraw.Response {
			return &httpraw.Response{Status: 200, Body: []byte("OK")}
		},
	}

	addr, stop := startTestServer(t, srv)
	defer stop()

	dialAndExchange := func(rawReq string) (statusCode int, rawResp string, err error) {
		conn, dialErr := net.DialTimeout("tcp", addr, 2*time.Second)
		if dialErr != nil {
			return 0, "", dialErr
		}
		defer conn.Close()

		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, writeErr := conn.Write([]byte(rawReq)); writeErr != nil {
			return 0, "", writeErr
		}

		reader := bufio.NewReader(conn)
		statusLine, readErr := reader.ReadString('\n')
		if readErr != nil {
			return 0, "", readErr
		}

		parts := strings.Split(strings.TrimSpace(statusLine), " ")
		if len(parts) >= 2 {
			code, convErr := strconv.Atoi(parts[1])
			if convErr == nil {
				statusCode = code
			}
		}

		rest, _ := io.ReadAll(reader)
		rawResp = statusLine + string(rest)
		return statusCode, rawResp, nil
	}

	tests := []struct {
		name         string
		rawReq       string
		expectStatus int
	}{
		{
			name:         "кривая стартовая строка (одно слово)",
			rawReq:       "GARBAGE\r\n\r\n",
			expectStatus: 400,
		},
		{
			name:         "кривая стартовая строка (два слова)",
			rawReq:       "GET /orders\r\n\r\n",
			expectStatus: 400,
		},
		{
			name:         "кривая стартовая строка (четыре слова)",
			rawReq:       "GET /orders HTTP/1.1 EXTRA\r\n\r\n",
			expectStatus: 400,
		},
		{
			name:         "версия не HTTP/1.1 (HTTP/1.0)",
			rawReq:       "GET /orders HTTP/1.0\r\nHost: localhost\r\n\r\n",
			expectStatus: 505,
		},
		{
			name:         "версия не HTTP/1.1 (HTTP/2.0)",
			rawReq:       "GET /orders HTTP/2.0\r\nHost: localhost\r\n\r\n",
			expectStatus: 505,
		},
		{
			name:         "заголовок без двоеточия",
			rawReq:       "GET /orders HTTP/1.1\r\nInvalidHeaderWithoutColon\r\n\r\n",
			expectStatus: 400,
		},
		{
			name: "оба заголовка длины одновременно (Content-Length и Transfer-Encoding)",
			rawReq: "POST /orders HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"Content-Length: 5\r\n" +
				"Transfer-Encoding: chunked\r\n\r\n" +
				"5\r\nhello\r\n0\r\n\r\n",
			expectStatus: 400,
		},
		{
			name: "стартовая строка и заголовки больше 8 КБ (431)",
			rawReq: "GET /orders HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"X-Huge-Header: " + strings.Repeat("A", 8200) + "\r\n\r\n",
			expectStatus: 431,
		},
		{
			name: "тело больше 1 МБ по Content-Length (413)",
			rawReq: "POST /upload HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"Content-Length: 1048577\r\n\r\n",
			expectStatus: 413,
		},
		{
			name: "тело больше 1 МБ в chunked (413)",
			rawReq: "POST /upload HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"Transfer-Encoding: chunked\r\n\r\n" +
				"100001\r\n", // 1048577 в hex
			expectStatus: 413,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, resp, err := dialAndExchange(tc.rawReq)
			if err != nil {
				t.Fatalf("exchange failed: %v", err)
			}
			if code != tc.expectStatus {
				t.Errorf("expected status %d, got %d. Response: %q", tc.expectStatus, code, resp)
			}
		})
	}
}

// 5. Медленный клиент. Подключился и молчит — соединение закрыто после ReadHeaderTimeout.
func TestServer_SlowClientTimeout(t *testing.T) {
	const shortTimeout = 80 * time.Millisecond

	srv := &httpraw.Server{
		ReadHeaderTimeout: shortTimeout,
		Handler: func(req *httpraw.Request) *httpraw.Response {
			return &httpraw.Response{Status: 200}
		},
	}

	addr, stop := startTestServer(t, srv)
	defer stop()

	start := time.Now()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	// Клиент подключился и молчит (ничего не отправляет).
	// Читаем из сокета в ожидании закрытия сервером по таймауту.
	buf := make([]byte, 128)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, readErr := conn.Read(buf)
	duration := time.Since(start)

	if readErr == nil && n > 0 {
		t.Errorf("expected EOF or closed connection, got %d bytes: %q", n, string(buf[:n]))
	}

	if !errors.Is(readErr, io.EOF) {
		// На некоторых ОС может возвращаться reset / closed error
		t.Logf("read returned error (expected disconnect): %v", readErr)
	}

	// Проверяем, что соединение закрылось примерно за ReadHeaderTimeout, а не висело вечно
	if duration < shortTimeout {
		t.Errorf("connection closed too fast: %v < %v", duration, shortTimeout)
	}
	if duration > 1*time.Second {
		t.Errorf("connection stayed open too long: %v", duration)
	}
}

// 6. Хендлер запаниковал — 500, сервер живёт дальше.
func TestServer_PanicRecovery(t *testing.T) {
	shouldPanic := true

	srv := &httpraw.Server{
		Handler: func(req *httpraw.Request) *httpraw.Response {
			if shouldPanic {
				panic("simulated disaster in handler")
			}
			return &httpraw.Response{
				Status: 200,
				Body:   []byte("recovered and healthy"),
			}
		},
	}

	addr, stop := startTestServer(t, srv)
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Запрос с паникой
	resp1, err := client.Get("http://" + addr + "/crash")
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	defer resp1.Body.Close()

	if resp1.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 Internal Server Error, got %d", resp1.StatusCode)
	}

	// 2. Сервер живёт дальше и обслуживает следующий запрос
	shouldPanic = false
	resp2, err := client.Get("http://" + addr + "/healthy")
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK after panic, got %d", resp2.StatusCode)
	}
	body, _ := io.ReadAll(resp2.Body)
	if string(body) != "recovered and healthy" {
		t.Errorf("unexpected body: %q", string(body))
	}
}

// 7. Клиент шлёт Connection: close — сервер отвечает Connection: close и закрывает соединение.
func TestServer_ConnectionClose(t *testing.T) {
	srv := &httpraw.Server{
		Handler: func(req *httpraw.Request) *httpraw.Response {
			return &httpraw.Response{
				Status: 200,
				Body:   []byte("closing soon"),
			}
		},
	}

	addr, stop := startTestServer(t, srv)
	defer stop()

	reqStr := "GET /close-me HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(reqStr)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	respStr := string(resp)
	if !strings.Contains(respStr, "Connection: close") {
		t.Errorf("expected response to contain 'Connection: close', got: %s", respStr)
	}
	if !strings.Contains(respStr, "closing soon") {
		t.Errorf("expected response to contain body 'closing soon', got: %s", respStr)
	}
}

// 8. Повторяющиеся заголовки склеиваются через ", ".
func TestServer_HeaderDuplicateJoining(t *testing.T) {
	var capturedHeaders map[string]string

	srv := &httpraw.Server{
		Handler: func(req *httpraw.Request) *httpraw.Response {
			capturedHeaders = req.Header
			return &httpraw.Response{Status: 200}
		},
	}

	addr, stop := startTestServer(t, srv)
	defer stop()

	reqStr := "GET /multi-headers HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Accept: text/html\r\n" +
		"Accept: application/xhtml+xml\r\n" +
		"Accept: application/xml;q=0.9\r\n\r\n"

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(reqStr)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	reader := bufio.NewReader(conn)
	_, _ = reader.ReadString('\n')

	expectedAccept := "text/html, application/xhtml+xml, application/xml;q=0.9"
	if capturedHeaders["accept"] != expectedAccept {
		t.Errorf("expected joined headers %q, got %q", expectedAccept, capturedHeaders["accept"])
	}
}

// 9. Проверка отсутствия запрещенных импортов в labs/httpraw.
func TestBannedImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("failed to glob files: %v", err)
	}

	banned := map[string]struct{}{
		`"net/http"`:          {},
		`"net/textproto"`:     {},
		`"net/http/httputil"`: {},
	}

	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("failed to read %s: %v", f, err)
		}
		node, err := parser.ParseFile(fset, f, data, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("failed to parse %s: %v", f, err)
		}
		for _, imp := range node.Imports {
			if _, isBanned := banned[imp.Path.Value]; isBanned {
				t.Errorf("file %s has banned import: %s", f, imp.Path.Value)
			}
		}
	}
}
