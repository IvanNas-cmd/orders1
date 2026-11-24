package httpraw

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// Serve принимает соединения, пока не отменят ctx. После отмены закрывает
// listener и возвращает nil.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.mu.Lock()
	s.conns = make(map[net.Conn]struct{})
	s.mu.Unlock()

	readHeaderTimeout := s.ReadHeaderTimeout
	if readHeaderTimeout <= 0 {
		readHeaderTimeout = defaultReadHeaderTimeout
	}
	idleTimeout := s.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = defaultIdleTimeout
	}

	var wg sync.WaitGroup
	defer func() {
		_ = ln.Close()
		s.closeAllConns()
		wg.Wait()
	}()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
		s.closeAllConns()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}

		s.trackConn(conn, true)
		wg.Add(1)

		go func(c net.Conn) {
			defer func() {
				_ = c.Close()
				s.trackConn(c, false)
				wg.Done()
			}()
			s.handleConn(c, readHeaderTimeout, idleTimeout)
		}(conn)
	}
}

func (s *Server) trackConn(c net.Conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if add {
		s.conns[c] = struct{}{}
	} else {
		delete(s.conns, c)
	}
}

func (s *Server) closeAllConns() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		_ = c.Close()
	}
}

func (s *Server) handleConn(conn net.Conn, readHeaderTimeout, idleTimeout time.Duration) {
	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)
	isFirstRequest := true

	for {
		if isFirstRequest {
			_ = conn.SetReadDeadline(time.Now().Add(readHeaderTimeout))
		} else {
			_ = conn.SetReadDeadline(time.Now().Add(idleTimeout))
			_, err := br.Peek(1)
			if err != nil {
				return // Клиент закрыл соединение или истек IdleTimeout
			}
			_ = conn.SetReadDeadline(time.Now().Add(readHeaderTimeout))
		}

		req, err := readRequestHeaders(br)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			var pErr *protoError
			if errors.As(err, &pErr) {
				_ = writeErrorResponse(bw, pErr.status, pErr.msg)
				return
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				// Клиент превысил таймаут на передачу заголовков — закрываем сокет без ответа
				return
			}
			_ = writeErrorResponse(bw, 400, "Bad Request")
			return
		}

		// Заголовки прочитаны — сбрасываем ReadDeadline перед чтением тела и вызовом хендлера
		_ = conn.SetReadDeadline(time.Time{})

		if err := readRequestBody(br, req); err != nil {
			var pErr *protoError
			if errors.As(err, &pErr) {
				_ = writeErrorResponse(bw, pErr.status, pErr.msg)
				return
			}
			_ = writeErrorResponse(bw, 400, "Bad Request")
			return
		}

		// Вызов хендлера с защитой от паники
		var res *Response
		func() {
			defer func() {
				if r := recover(); r != nil {
					res = &Response{
						Status: 500,
						Header: map[string]string{
							"Content-Type": "text/plain; charset=utf-8",
							"Connection":   "close",
						},
						Body: []byte("500 Internal Server Error\n"),
					}
				}
			}()
			if s.Handler != nil {
				res = s.Handler(req)
			}
		}()

		if res == nil {
			res = &Response{Status: 200}
		}

		shouldClose := isCloseConnection(req, res)
		if res.Status >= 400 {
			shouldClose = true
		}

		if err := writeResponse(bw, res, shouldClose); err != nil {
			return
		}

		if shouldClose {
			return
		}

		isFirstRequest = false
	}
}
