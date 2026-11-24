package httpraw

import (
	"fmt"
	"time"
)

const (
	maxHeaderBytes           = 8 * 1024    // 8 KB
	maxBodyBytes             = 1024 * 1024 // 1 MB
	defaultReadHeaderTimeout = 5 * time.Second
	defaultIdleTimeout       = 30 * time.Second
	timeFormatRFC1123        = "Mon, 02 Jan 2006 15:04:05 GMT"
)

type protoError struct {
	status int
	msg    string
}

func (e *protoError) Error() string {
	return fmt.Sprintf("httpraw: status %d: %s", e.status, e.msg)
}

func statusText(code int) string {
	switch code {
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 202:
		return "Accepted"
	case 204:
		return "No Content"
	case 301:
		return "Moved Permanently"
	case 302:
		return "Found"
	case 304:
		return "Not Modified"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 413:
		return "Content Too Large"
	case 431:
		return "Request Header Fields Too Large"
	case 500:
		return "Internal Server Error"
	case 501:
		return "Not Implemented"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	case 505:
		return "HTTP Version Not Supported"
	default:
		return "Unknown"
	}
}
