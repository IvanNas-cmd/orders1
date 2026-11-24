package httpraw

import (
	"net"
	"sync"
	"time"
)

// Request — разобранный запрос.
type Request struct {
	Method string            // "GET", "POST", ...
	Target string            // как пришло: "/orders/1?x=y"
	Header map[string]string // имена в нижнем регистре; повторы склеены через ", "
	Body   []byte            // уже раскодированное, если пришло chunked
}

// Response — ответ хендлера.
type Response struct {
	Status int
	Header map[string]string
	Body   []byte
	// Chunks — если не nil, ответ уходит с Transfer-Encoding: chunked,
	// каждый элемент — отдельным чанком. Body тогда игнорируется.
	Chunks [][]byte
}

type Handler func(*Request) *Response

type Server struct {
	Handler           Handler
	ReadHeaderTimeout time.Duration // 0 — значит 5s
	IdleTimeout       time.Duration // 0 — значит 30s

	mu    sync.Mutex
	conns map[net.Conn]struct{}
}
