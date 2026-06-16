package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type correlationKeyType string

const CorrelationIDKey correlationKeyType = "correlation_id"

// ContextWithCorrelationID attaches a correlation ID to the context
func ContextWithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, CorrelationIDKey, correlationID)
}

// CorrelationIDFromContext retrieves the correlation ID from the context
func CorrelationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(CorrelationIDKey).(string); ok {
		return v
	}
	return ""
}

type AuditingTransport struct {
	Base http.RoundTripper
}

var (
	seqMap   = make(map[string]*uint64)
	seqMutex sync.Mutex
)

func getNextSeq(correlationID string) uint64 {
	seqMutex.Lock()
	defer seqMutex.Unlock()

	counter, ok := seqMap[correlationID]
	if !ok {
		var val uint64 = 0
		counter = &val
		seqMap[correlationID] = counter
	}
	return atomic.AddUint64(counter, 1)
}

func getLogDir(correlationID string) string {
	baseDir := "/app"
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		if _, err := os.Stat("web"); err == nil {
			baseDir = "web"
		} else {
			baseDir = "."
		}
	}
	return filepath.Join(baseDir, "logs", "sync_runs", correlationID)
}

func getProvider(req *http.Request) string {
	provider := "unknown"
	if req.URL != nil {
		host := strings.ToLower(req.URL.Host)
		if strings.Contains(host, "gocardless") {
			provider = "gocardless"
		} else if strings.Contains(host, "trading212") {
			provider = "trading212"
		} else if strings.Contains(host, "enablebanking") {
			provider = "enablebanking"
		}
	}
	return provider
}

func redactJSON(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		newMap := make(map[string]interface{})
		for k, v := range val {
			kl := strings.ToLower(k)
			if strings.Contains(kl, "key") ||
				strings.Contains(kl, "secret") ||
				strings.Contains(kl, "password") ||
				strings.Contains(kl, "token") ||
				strings.Contains(kl, "pwd") ||
				strings.Contains(kl, "signature") ||
				strings.Contains(kl, "cert") ||
				kl == "user" {
				newMap[k] = "[REDACTED]"
			} else {
				newMap[k] = redactJSON(v)
			}
		}
		return newMap
	case []interface{}:
		newSlice := make([]interface{}, len(val))
		for i, item := range val {
			newSlice[i] = redactJSON(item)
		}
		return newSlice
	default:
		return v
	}
}

func redactBody(body []byte, contentType string) []byte {
	if len(body) == 0 {
		return body
	}
	if strings.Contains(strings.ToLower(contentType), "application/json") {
		var parsed interface{}
		if err := json.Unmarshal(body, &parsed); err == nil {
			redacted := redactJSON(parsed)
			if b, err := json.MarshalIndent(redacted, "", "  "); err == nil {
				return b
			}
		}
	} else if strings.Contains(strings.ToLower(contentType), "application/x-www-form-urlencoded") {
		if values, err := url.ParseQuery(string(body)); err == nil {
			for k := range values {
				kl := strings.ToLower(k)
				if strings.Contains(kl, "key") ||
					strings.Contains(kl, "secret") ||
					strings.Contains(kl, "password") ||
					strings.Contains(kl, "token") ||
					strings.Contains(kl, "pwd") ||
					strings.Contains(kl, "signature") ||
					strings.Contains(kl, "cert") ||
					kl == "user" {
					values.Set(k, "[REDACTED]")
				}
			}
			return []byte(values.Encode())
		}
	}
	return body
}

func redactHeaders(headers http.Header) http.Header {
	newHeaders := make(http.Header)
	for k, v := range headers {
		kl := strings.ToLower(k)
		if strings.Contains(kl, "auth") ||
			strings.Contains(kl, "key") ||
			strings.Contains(kl, "secret") ||
			strings.Contains(kl, "token") ||
			strings.Contains(kl, "password") ||
			strings.Contains(kl, "cookie") {
			newHeaders[k] = []string{"[REDACTED]"}
		} else {
			newHeaders[k] = v
		}
	}
	return newHeaders
}

func redactURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	q := u.Query()
	modified := false
	for k := range q {
		kl := strings.ToLower(k)
		if strings.Contains(kl, "key") ||
			strings.Contains(kl, "secret") ||
			strings.Contains(kl, "password") ||
			strings.Contains(kl, "token") ||
			strings.Contains(kl, "pwd") ||
			strings.Contains(kl, "signature") {
			q.Set(k, "[REDACTED]")
			modified = true
		}
	}
	if modified {
		uCopy := *u
		uCopy.RawQuery = q.Encode()
		return uCopy.String()
	}
	return u.String()
}

type ReqDump struct {
	Method    string              `json:"method"`
	URL       string              `json:"url"`
	Headers   map[string][]string `json:"headers"`
	Body      interface{}         `json:"body,omitempty"`
	RawBody   string              `json:"raw_body,omitempty"`
	Timestamp string              `json:"timestamp"`
	Error     string              `json:"error,omitempty"`
}

type RespDump struct {
	StatusCode int                 `json:"status_code"`
	Status     string              `json:"status"`
	Headers    map[string][]string `json:"headers"`
	Body       interface{}         `json:"body,omitempty"`
	RawBody    string              `json:"raw_body,omitempty"`
	Timestamp  string              `json:"timestamp"`
}

func (t *AuditingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	correlationID := CorrelationIDFromContext(ctx)

	if correlationID == "" {
		base := t.Base
		if base == nil {
			base = http.DefaultTransport
		}
		return base.RoundTrip(req)
	}

	seq := getNextSeq(correlationID)
	provider := getProvider(req)

	// Read request body
	var reqBodyBytes []byte
	if req.Body != nil {
		var err error
		reqBodyBytes, err = io.ReadAll(req.Body)
		if err == nil {
			req.Body = io.NopCloser(bytes.NewBuffer(reqBodyBytes))
		}
	}

	// Perform request
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}

	startTime := time.Now()
	resp, err := base.RoundTrip(req)
	endTime := time.Now()

	if err != nil {
		log.Printf("[AUDIT][%s] %s %s FAILED: %v", correlationID, req.Method, redactURL(req.URL), err)
	} else if resp != nil {
		log.Printf("[AUDIT][%s] %s %s [%s]", correlationID, req.Method, redactURL(req.URL), resp.Status)
	}

	// Ensure the logs folder exists
	logDir := getLogDir(correlationID)
	if errMk := os.MkdirAll(logDir, 0755); errMk != nil {
		log.Printf("[AUDIT] Failed to create log directory: %v", errMk)
	}

	// Write Request Dump
	reqDump := ReqDump{
		Method:    req.Method,
		URL:       redactURL(req.URL),
		Headers:   redactHeaders(req.Header),
		Timestamp: startTime.Format(time.RFC3339),
	}
	if err != nil {
		reqDump.Error = err.Error()
	}

	redactedReqBody := redactBody(reqBodyBytes, req.Header.Get("Content-Type"))
	var reqBodyObj interface{}
	if errU := json.Unmarshal(redactedReqBody, &reqBodyObj); errU == nil {
		reqDump.Body = reqBodyObj
	} else if len(redactedReqBody) > 0 {
		reqDump.RawBody = string(redactedReqBody)
	}

	reqFilename := filepath.Join(logDir, fmt.Sprintf("%03d_%s_req.json", seq, provider))
	if reqJSON, errM := json.MarshalIndent(reqDump, "", "  "); errM == nil {
		_ = os.WriteFile(reqFilename, reqJSON, 0644)
	}

	if err != nil {
		return nil, err
	}

	// Read response body
	var respBodyBytes []byte
	if resp.Body != nil {
		var rerr error
		respBodyBytes, rerr = io.ReadAll(resp.Body)
		if rerr == nil {
			resp.Body = io.NopCloser(bytes.NewBuffer(respBodyBytes))
		}
	}

	// Write Response Dump
	respDump := RespDump{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Headers:    redactHeaders(resp.Header),
		Timestamp:  endTime.Format(time.RFC3339),
	}

	redactedRespBody := redactBody(respBodyBytes, resp.Header.Get("Content-Type"))
	var respBodyObj interface{}
	if errU := json.Unmarshal(redactedRespBody, &respBodyObj); errU == nil {
		respDump.Body = respBodyObj
	} else if len(redactedRespBody) > 0 {
		respDump.RawBody = string(redactedRespBody)
	}

	respFilename := filepath.Join(logDir, fmt.Sprintf("%03d_%s_resp.json", seq, provider))
	if respJSON, errM := json.MarshalIndent(respDump, "", "  "); errM == nil {
		_ = os.WriteFile(respFilename, respJSON, 0644)
	}

	return resp, nil
}
