package checker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/juju/ratelimit"
)

type ProxyClient struct {
	Client    *http.Client
	BytesRead *uint64
	proxy     string
}

// statsConn 网络层连接包装器，用于追踪实际网络传输字节数并实现速率限制
type statsConn struct {
	net.Conn
	bytesCounter *uint64
	bucket       *ratelimit.Bucket
}

func (c *statsConn) Read(p []byte) (n int, err error) {
	n, err = c.Conn.Read(p)
	if n > 0 {
		atomic.AddUint64(c.bytesCounter, uint64(n))
		// 速率限制：通过全局 Bucket 控制读取速率（网络层）
		if c.bucket != nil {
			c.bucket.Wait(int64(n))
		}
	}
	return
}

func (c *statsConn) Write(p []byte) (n int, err error) {
	n, err = c.Conn.Write(p)
	if n > 0 {
		// 写入字节数不计入 BytesRead（BytesRead 仅统计下载流量）
		// 速率限制同样应用于写入
		if c.bucket != nil {
			c.bucket.Wait(int64(n))
		}
	}
	return
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

	bytesRead := new(uint64)

	transport := &http.Transport{}
	if parsed, err := url.Parse(proxyURL); err == nil {
		transport.Proxy = http.ProxyURL(parsed)
	}
	transport.DisableKeepAlives = true
	transport.ForceAttemptHTTP2 = false
	transport.MaxIdleConns = 0
	transport.MaxIdleConnsPerHost = 0

	// 自定义 DialContext：包装连接为 statsConn，追踪网络层字节数并实现速率限制
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		conn, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &statsConn{
			Conn:         conn,
			bytesCounter: bytesRead,
			bucket:       Bucket,
		}, nil
	}

	return &ProxyClient{
		Client:    &http.Client{Transport: transport},
		BytesRead: bytesRead,
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