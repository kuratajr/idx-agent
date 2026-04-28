package clientcore

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nezhahq/agent/pkg/xtprocore/tunnel"
)

func (c *runtimeClient) handleHTTPRequest(msg tunnel.Message) {
	if c.protocol != ProtocolHTTP {
		return
	}
	scheme := "http"
	if strings.HasSuffix(c.localAddr, ":443") {
		scheme = "https"
	}
	req, err := http.NewRequest(msg.Method, fmt.Sprintf("%s://%s%s", scheme, c.localAddr, msg.Path), bytes.NewReader(msg.Body))
	if err != nil {
		c.sendHTTPError(msg.ID, http.StatusBadGateway, err.Error())
		return
	}
	for key, value := range msg.Headers {
		req.Header.Set(key, value)
	}
	if strings.Contains(c.localAddr, "127.0.0.1") || strings.Contains(c.localAddr, "::1") || strings.Contains(c.localAddr, "localhost") {
		req.Host = "localhost"
	} else {
		req.Host = c.localAddr
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	httpClient := &http.Client{Timeout: 30 * time.Second, Transport: tr}

	resp, err := httpClient.Do(req)
	if err != nil {
		c.sendHTTPError(msg.ID, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.sendHTTPError(msg.ID, http.StatusInternalServerError, err.Error())
		return
	}
	headers := make(map[string]string, len(resp.Header))
	for key, values := range resp.Header {
		headers[key] = strings.Join(values, ", ")
	}
	_ = c.enc.Encode(tunnel.Message{
		Type:       "http_response",
		ID:         msg.ID,
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       body,
	})
	atomic.AddUint64(&c.bytesDown, uint64(len(msg.Body)))
	atomic.AddUint64(&c.bytesUp, uint64(len(body)))
}

func (c *runtimeClient) sendHTTPError(requestID string, statusCode int, errorMsg string) {
	if c.enc == nil {
		return
	}
	_ = c.enc.Encode(tunnel.Message{
		Type:       "http_response",
		ID:         requestID,
		StatusCode: statusCode,
		Headers:    map[string]string{"Content-Type": "text/plain"},
		Body:       []byte(errorMsg),
	})
}
