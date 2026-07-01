package checker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"log/slog"

	"github.com/55gY/subs-check/config"
	"github.com/metacubex/mihomo/common/convert"
)

const minEffectiveSpeedDuration = 120 * time.Millisecond

type SpeedCheckMetrics struct {
	SpeedKBps          int
	ActualBytes        int64
	TotalDuration      time.Duration
	ResponseWait       time.Duration
	DownloadDuration   time.Duration
	SampleDuration     time.Duration
	LowConfidence      bool
	SampleTooSmall     bool
	ContextCanceled    bool
	ResponseStatusCode int
}

// networkLimitedReader 基于网络层字节计数器的大小限制 reader
type networkLimitedReader struct {
	reader       io.Reader
	bytesCounter *uint64
	startBytes   uint64
	limit        uint64
}

func (r *networkLimitedReader) Read(p []byte) (n int, err error) {
	if r.limit > 0 {
		currentBytes := atomic.LoadUint64(r.bytesCounter)
		networkRead := currentBytes - r.startBytes

		if networkRead >= r.limit {
			return 0, io.EOF
		}

		// 限制本次读取的大小（粗略控制，因为网络层可能读取更多）
		if remaining := r.limit - networkRead; remaining < uint64(len(p)) {
			p = p[:remaining]
		}
	}
	return r.reader.Read(p)
}

func CheckSpeed(ctx context.Context, httpClient *http.Client, bytesCounter *uint64) (SpeedCheckMetrics, error) {
	metrics := SpeedCheckMetrics{}
	defer func() {
		if r := recover(); r != nil {
			slog.Debug(fmt.Sprintf("CheckSpeed发生panic: %v", r))
		}
	}()

	// 注意：速度限制在网络层（statsConn）实现，大小限制在应用层基于网络字节计数器实现
	// - 速度限制：通过 bucket 在 statsConn 中实现（网络层）
	// - 大小限制：通过 networkLimitedReader 基于网络字节计数器实现（应用层，但限制网络流量）

	// 创建一个新的测速专用客户端，基于原有客户端的传输层
	speedClient := &http.Client{
		// 设置更长的超时时间用于测速
		Timeout: time.Duration(config.GlobalConfig.DownloadTimeout) * time.Second,
		// 保持原有的传输层配置
		Transport: httpClient.Transport,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", config.GlobalConfig.SpeedTestUrl, nil)
	if err != nil {
		return metrics, err
	}
	req.Header.Set("User-Agent", convert.RandUserAgent())

	// 记录测速前的网络传输字节数
	var startBytes uint64
	if bytesCounter != nil {
		startBytes = atomic.LoadUint64(bytesCounter)
	}
	startTime := time.Now()

	resp, err := speedClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			metrics.ContextCanceled = true
		}
		slog.Debug(fmt.Sprintf("测速请求失败: %v", err))
		return metrics, err
	}
	metrics.ResponseWait = time.Since(startTime)
	metrics.ResponseStatusCode = resp.StatusCode
	defer func() {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
	}()

	// 计算网络层的大小限制
	var limitSize uint64
	if config.GlobalConfig.DownloadMB > 0 {
		limitSize = uint64(config.GlobalConfig.DownloadMB) * 1024 * 1024
	} else {
		limitSize = 0 // 不限制
	}

	// 使用 networkLimitedReader 包装响应体，基于网络字节计数器限制大小
	limitedReader := &networkLimitedReader{
		reader:       resp.Body,
		bytesCounter: bytesCounter,
		startBytes:   startBytes,
		limit:        limitSize,
	}

	// 读取所有数据
	downloadStart := time.Now()
	totalBytes, err := io.Copy(io.Discard, limitedReader)
	metrics.DownloadDuration = time.Since(downloadStart)
	// io.EOF 是正常的（达到限制），其他错误才需要关注
	if err != nil && err != io.EOF && totalBytes == 0 {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			metrics.ContextCanceled = true
		}
		slog.Debug(fmt.Sprintf("totalBytes: %d, 读取数据时发生错误: %v", totalBytes, err))
		return metrics, err
	}

	metrics.TotalDuration = time.Since(startTime)
	metrics.SampleDuration = metrics.DownloadDuration
	if metrics.SampleDuration <= 0 {
		metrics.SampleDuration = metrics.TotalDuration
	}

	// 计算实际网络传输的字节数（压缩数据）
	var actualBytes int64
	if bytesCounter != nil {
		actualBytes = int64(atomic.LoadUint64(bytesCounter) - startBytes)
	} else {
		// 如果没有字节计数器，无法获取准确数据
		actualBytes = 0
	}
	metrics.ActualBytes = actualBytes

	if actualBytes <= 32*1024 || totalBytes <= 32*1024 {
		metrics.SampleTooSmall = true
	}
	if metrics.SampleDuration < minEffectiveSpeedDuration {
		metrics.LowConfidence = true
	}

	durationMs := metrics.SampleDuration.Milliseconds()
	if durationMs <= 0 {
		durationMs = 1
	}

	// 计算速度（KB/s），优先使用实际网络传输字节数，fallback到io.Copy读取的字节数
	speedBytes := actualBytes
	if speedBytes <= 0 {
		speedBytes = totalBytes
	}
	metrics.SpeedKBps = int(float64(speedBytes) / 1024 * 1000 / float64(durationMs))

	// 调试日志：输出字节统计详情，便于排查流量统计问题
	slog.Debug(fmt.Sprintf("测速字节统计: actualBytes=%d totalBytes=%d speedBytes=%d durationMs=%d speedKBps=%d",
		actualBytes, totalBytes, speedBytes, durationMs, metrics.SpeedKBps))

	// 更新全局流量统计
	if speedBytes > 0 {
		TotalBytes.Add(uint64(speedBytes))
	}

	return metrics, nil
}
