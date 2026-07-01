package checker

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type ProxyClient struct {
	Client    *http.Client
	BytesRead *uint64
	proxy     string
}

func CreateClient(proxyMap map[string]any) *ProxyClient {
	host, _ := proxyMap["server"].(string)
	if strings.TrimSpace(host) == "" {
		return nil
	}
	port := 0
	switch v := proxyMap["port"].(type) {
	case int:
		port = v
	case int32:
		port = int(v)
	case int64:
		port = int(v)
	case uint:
		port = int(v)
	case uint32:
		port = int(v)
	case uint64:
		port = int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			port = n
		}
	}
	if port <= 0 {
		return nil
	}

	proxyURL := fmt.Sprintf("http://%s:%d", host, port)

	transport := &http.Transport{}
	if parsed, err := url.Parse(proxyURL); err == nil {
		transport.Proxy = http.ProxyURL(parsed)
	}
	transport.DisableKeepAlives = true
	transport.ForceAttemptHTTP2 = false
	transport.MaxIdleConns = 0
	transport.MaxIdleConnsPerHost = 0

	return &ProxyClient{
		Client:    &http.Client{Transport: transport},
		BytesRead: new(uint64),
		proxy:     proxyURL,
	}
}

func (c *ProxyClient) Close() {
	if c.Client != nil {
		// 关闭Transport的空闲连接，释放网络资源
		if transport, ok := c.Client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
		c.Client.CloseIdleConnections()
	}
}
