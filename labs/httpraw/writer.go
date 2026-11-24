package httpraw

import (
	"bufio"
	"fmt"
	"strings"
	"time"
)

// writeResponse записывает HTTP/1.1 ответ клиенту.
func writeResponse(bw *bufio.Writer, res *Response, shouldClose bool) error {
	status := res.Status
	if status == 0 {
		status = 200
	}

	// 1. Стартовая строка ответа
	if _, err := fmt.Fprintf(bw, "HTTP/1.1 %d %s\r\n", status, statusText(status)); err != nil {
		return err
	}

	// 2. Transfer-Encoding или Content-Length
	if res.Chunks != nil {
		if _, err := bw.WriteString("Transfer-Encoding: chunked\r\n"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(bw, "Content-Length: %d\r\n", len(res.Body)); err != nil {
			return err
		}
	}

	// 3. Заголовок Connection
	if shouldClose {
		if _, err := bw.WriteString("Connection: close\r\n"); err != nil {
			return err
		}
	} else {
		if _, err := bw.WriteString("Connection: keep-alive\r\n"); err != nil {
			return err
		}
	}

	// 4. Заголовок Date (если не задан в res.Header)
	hasDate := false
	for k := range res.Header {
		if strings.EqualFold(k, "date") {
			hasDate = true
			break
		}
	}
	if !hasDate {
		dateStr := time.Now().UTC().Format(timeFormatRFC1123)
		if _, err := fmt.Fprintf(bw, "Date: %s\r\n", dateStr); err != nil {
			return err
		}
	}

	// 5. Заголовки хендлера
	for k, v := range res.Header {
		if strings.EqualFold(k, "content-length") ||
			strings.EqualFold(k, "transfer-encoding") ||
			strings.EqualFold(k, "connection") {
			continue
		}
		if _, err := fmt.Fprintf(bw, "%s: %s\r\n", k, v); err != nil {
			return err
		}
	}

	// 6. Конец секции заголовков
	if _, err := bw.WriteString("\r\n"); err != nil {
		return err
	}

	// 7. Тело или чанки
	if res.Chunks != nil {
		for _, chunk := range res.Chunks {
			if _, err := fmt.Fprintf(bw, "%x\r\n", len(chunk)); err != nil {
				return err
			}
			if _, err := bw.Write(chunk); err != nil {
				return err
			}
			if _, err := bw.WriteString("\r\n"); err != nil {
				return err
			}
		}
		if _, err := bw.WriteString("0\r\n\r\n"); err != nil {
			return err
		}
	} else if len(res.Body) > 0 {
		if _, err := bw.Write(res.Body); err != nil {
			return err
		}
	}

	return bw.Flush()
}

// writeErrorResponse формирует и отправляет ответ с кодом ошибки и закрытием соединения.
func writeErrorResponse(bw *bufio.Writer, status int, msg string) error {
	body := []byte(fmt.Sprintf("%d %s\n", status, statusText(status)))
	res := &Response{
		Status: status,
		Header: map[string]string{
			"Content-Type": "text/plain; charset=utf-8",
		},
		Body: body,
	}
	return writeResponse(bw, res, true)
}

// isCloseConnection определяет, нужно ли закрыть TCP-соединение после ответа.
func isCloseConnection(req *Request, res *Response) bool {
	if req != nil {
		if strings.Contains(strings.ToLower(req.Header["connection"]), "close") {
			return true
		}
	}
	if res != nil {
		for k, v := range res.Header {
			if strings.EqualFold(k, "connection") && strings.Contains(strings.ToLower(v), "close") {
				return true
			}
		}
	}
	return false
}
