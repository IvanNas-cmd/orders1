package httpraw

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// readHeaderLine читает одну строку до \n, проверяя суммарный лимит байт limit.
func readHeaderLine(br *bufio.Reader, totalRead *int, limit int) (string, error) {
	var line []byte
	for {
		b, err := br.ReadByte()
		if err != nil {
			return "", err
		}
		*totalRead++
		if *totalRead > limit {
			return "", &protoError{status: 431, msg: "request header fields too large"}
		}
		if b == '\n' {
			break
		}
		line = append(line, b)
	}

	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	return string(line), nil
}

// readRequestHeaders читает и парсит стартовую строку и заголовки запроса в пределах 8 КБ.
func readRequestHeaders(br *bufio.Reader) (*Request, error) {
	totalRead := 0

	var reqLine string
	for {
		line, err := readHeaderLine(br, &totalRead, maxHeaderBytes)
		if err != nil {
			return nil, err
		}
		// Пропускаем пустые строки перед запросом (RFC 9112 Section 2.2)
		if line != "" {
			reqLine = line
			break
		}
	}

	parts := strings.Split(reqLine, " ")
	if len(parts) != 3 {
		return nil, &protoError{status: 400, msg: "malformed request line"}
	}

	method, target, proto := parts[0], parts[1], parts[2]
	if method == "" || target == "" {
		return nil, &protoError{status: 400, msg: "empty method or target"}
	}

	if proto != "HTTP/1.1" {
		return nil, &protoError{status: 505, msg: "http version not supported"}
	}

	headers := make(map[string]string)
	for {
		line, err := readHeaderLine(br, &totalRead, maxHeaderBytes)
		if err != nil {
			return nil, err
		}
		if line == "" {
			break // пустая строка — конец заголовков
		}

		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			return nil, &protoError{status: 400, msg: "header line missing colon or empty name"}
		}

		rawName := line[:colon]
		if strings.ContainsAny(rawName, " \t") {
			return nil, &protoError{status: 400, msg: "whitespace in header name"}
		}

		key := strings.ToLower(strings.TrimSpace(rawName))
		val := strings.TrimSpace(line[colon+1:])

		if prev, ok := headers[key]; ok {
			headers[key] = prev + ", " + val
		} else {
			headers[key] = val
		}
	}

	return &Request{
		Method: method,
		Target: target,
		Header: headers,
	}, nil
}

// readRequestBody считывает тело запроса по Content-Length или Transfer-Encoding: chunked.
func readRequestBody(br *bufio.Reader, req *Request) error {
	_, hasCL := req.Header["content-length"]
	_, hasTE := req.Header["transfer-encoding"]

	if hasCL && hasTE {
		return &protoError{status: 400, msg: "both content-length and transfer-encoding present"}
	}

	if hasTE {
		te := strings.ToLower(req.Header["transfer-encoding"])
		if !strings.Contains(te, "chunked") {
			return &protoError{status: 400, msg: "unsupported transfer-encoding"}
		}
		body, err := readChunkedBody(br, maxBodyBytes)
		if err != nil {
			return err
		}
		req.Body = body
		return nil
	}

	if hasCL {
		clStr := req.Header["content-length"]
		if strings.Contains(clStr, ",") {
			parts := strings.Split(clStr, ",")
			first := strings.TrimSpace(parts[0])
			for _, p := range parts[1:] {
				if strings.TrimSpace(p) != first {
					return &protoError{status: 400, msg: "conflicting content-length values"}
				}
			}
			clStr = first
		}

		cl, err := strconv.ParseInt(strings.TrimSpace(clStr), 10, 64)
		if err != nil || cl < 0 {
			return &protoError{status: 400, msg: "invalid content-length"}
		}

		if cl > maxBodyBytes {
			return &protoError{status: 413, msg: "content too large"}
		}

		if cl > 0 {
			req.Body = make([]byte, cl)
			if _, err := io.ReadFull(br, req.Body); err != nil {
				return err
			}
		}
	}

	return nil
}

// readChunkedBody раскодирует тело запроса с Transfer-Encoding: chunked.
func readChunkedBody(br *bufio.Reader, maxBytes int) ([]byte, error) {
	var body []byte

	for {
		// Читаем строку размера чанка
		var sizeLine []byte
		for {
			b, err := br.ReadByte()
			if err != nil {
				return nil, err
			}
			if b == '\n' {
				break
			}
			sizeLine = append(sizeLine, b)
		}

		if len(sizeLine) > 0 && sizeLine[len(sizeLine)-1] == '\r' {
			sizeLine = sizeLine[:len(sizeLine)-1]
		}

		rawSize := string(sizeLine)
		if idx := strings.IndexByte(rawSize, ';'); idx >= 0 {
			rawSize = rawSize[:idx]
		}
		rawSize = strings.TrimSpace(rawSize)

		chunkSize, err := strconv.ParseUint(rawSize, 16, 64)
		if err != nil {
			return nil, &protoError{status: 400, msg: "invalid chunk size hex"}
		}

		if chunkSize == 0 {
			// Терминальный чанк — читаем трейлеры до пустой строки
			for {
				var tline []byte
				for {
					b, err := br.ReadByte()
					if err != nil {
						return nil, err
					}
					if b == '\n' {
						break
					}
					tline = append(tline, b)
				}
				if len(tline) > 0 && tline[len(tline)-1] == '\r' {
					tline = tline[:len(tline)-1]
				}
				if len(tline) == 0 {
					break
				}
			}
			break
		}

		if uint64(len(body))+chunkSize > uint64(maxBytes) {
			return nil, &protoError{status: 413, msg: "content too large"}
		}

		chunk := make([]byte, chunkSize)
		if _, err := io.ReadFull(br, chunk); err != nil {
			return nil, err
		}

		// За данными чанка обязательно идет CRLF
		crlf := make([]byte, 2)
		if _, err := io.ReadFull(br, crlf); err != nil {
			return nil, err
		}
		if crlf[0] != '\r' || crlf[1] != '\n' {
			return nil, &protoError{status: 400, msg: "malformed chunk: missing CRLF"}
		}

		body = append(body, chunk...)
	}

	return body, nil
}
