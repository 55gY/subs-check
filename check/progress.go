package check

import (
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/55gY/subs-check/config"
)

// 默认使用动态权重显示进度条
var progressAlgorithm ProgressAlgorithm

func init() {
	// 暂时默认使用动态权重，因为config中可能没有ProgressMode字段
	progressAlgorithm = DynamicWeightProgress
}

// ProgressAlgorithm 切换进度显示
type ProgressAlgorithm int

const (
	DynamicWeightProgress ProgressAlgorithm = iota // 总数恒等于节点总数，按权重映射百分比
	StagePriorityProgress                          // 阶段优先：显示当前阶段完成/阶段总
)

// ProgressWeight 不同检测阶段的进度权重
type ProgressWeight struct {
	alive float64
	speed float64
	media float64
}

// ProgressTracker 存储每个阶段的检测进度信息
type ProgressTracker struct {
	// 总任务数
	totalJobs atomic.Int32

	// 已检测数量
	aliveDone atomic.Int32
	speedDone atomic.Int32
	mediaDone atomic.Int32

	// 成功数量
	aliveSuccess atomic.Int32
	speedSuccess atomic.Int32

	// 当前处于 测活-测速-媒体检测 阶段
	currentStage atomic.Int32

	// 确保进度条输出完成
	finalized atomic.Bool
}

// NewProgressTracker 初始化进度追踪器并重置外部原子变量。
func NewProgressTracker(total int) *ProgressTracker {
	pt := &ProgressTracker{}
	if total > math.MaxInt32 {
		total = math.MaxInt32
	}
	pt.totalJobs.Store(int32(total))
	pt.currentStage.Store(0)

	ProxyCount.Store(uint32(total))
	Progress.Store(0)

	// 初始化进度权重
	// 注意：speedON 和 mediaON 需要在 check.go 中定义或传递
	// 这里暂时假设它们是全局变量或通过参数传递，稍后在 check.go 中修复
	// 为了避免循环依赖，这里先不调用 getCheckWeight，而是在 check.go 中初始化
	return pt
}

// getCheckWeight 根据启用的检查来确定进度权重的分配。
func getCheckWeight(speedON, mediaON bool) ProgressWeight {
	w := ProgressWeight{alive: 85, speed: 10, media: 5} // 默认权重 (全部开启时)

	switch {
	case !speedON && !mediaON:
		w = ProgressWeight{alive: 100, speed: 0, media: 0}
	case !speedON:
		w = ProgressWeight{alive: 80, speed: 0, media: 20}
	case !mediaON:
		w = ProgressWeight{alive: 70, speed: 30, media: 0}
	}

	return w
}

// CountAlive 标记一个存活检测已完成，并更新进度。
func (pt *ProgressTracker) CountAlive(success bool) {
	pt.aliveDone.Add(1)
	if success {
		pt.aliveSuccess.Add(1)
	}
	pt.refresh()
}

// CountSpeed 标记一个测速已完成，并更新进度。
func (pt *ProgressTracker) CountSpeed(success bool) {
	pt.speedDone.Add(1)
	if success {
		pt.speedSuccess.Add(1)
	}
	pt.refresh()
}

// CountMedia 标记一个媒体检测已完成，并更新进度。
func (pt *ProgressTracker) CountMedia() {
	pt.mediaDone.Add(1)
	pt.refresh()
}

// refresh 计算并更新全局进度。
func (pt *ProgressTracker) refresh() {
	total := float64(pt.totalJobs.Load())
	if total == 0 {
		return
	}

	// 计算各阶段完成百分比 (0.0 - 1.0)
	// 注意：分母是 total，意味着假设所有节点都通过了前一阶段
	// 实际上，后续阶段的任务数会少于 total，但这正是动态权重的意义所在：
	// 失败的节点在早期阶段就被视为“完成了后续所有阶段的进度贡献”
	
	// 修正逻辑：
	// 存活检测失败的节点，视为完成了测速和媒体检测
	// 测速失败的节点，视为完成了媒体检测
	
	aliveDone := float64(pt.aliveDone.Load())
	aliveFail := aliveDone - float64(pt.aliveSuccess.Load())
	
	speedDone := float64(pt.speedDone.Load())
	speedFail := speedDone - float64(pt.speedSuccess.Load())
	
	mediaDone := float64(pt.mediaDone.Load())

	// 计算加权进度
	// 存活阶段贡献：(已测活 / 总数) * 权重
	pAlive := (aliveDone / total) * progressWeight.alive

	// 测速阶段贡献：((已测速 + 存活失败) / 总数) * 权重
	// 解释：存活失败的节点虽然没测速，但它对测速阶段进度的贡献是“已处理”
	pSpeed := ((speedDone + aliveFail) / total) * progressWeight.speed

	// 媒体阶段贡献：((已媒体 + 测速失败 + 存活失败) / 总数) * 权重
	pMedia := ((mediaDone + speedFail + aliveFail) / total) * progressWeight.media

	currentProgress := pAlive + pSpeed + pMedia
	
	// 限制在 100% 以内
	if currentProgress > 100 {
		currentProgress = 100
	}

	// 更新全局原子变量，供 showProgress 读取
	// Progress 存储的是百分比 * 100 (即 0-10000，如果需要更高精度)
	// 但原代码 showProgress 似乎是用 current / total * 100
	// 这里我们需要适配原有的 showProgress 或者重写它
	// 假设我们重写 showProgress 来直接读取 Progress 作为百分比
	// 或者我们这里反向计算出一个虚拟的 "current" 值
	
	virtualCurrent := int32(currentProgress / 100.0 * total)
	Progress.Store(uint32(virtualCurrent))
	
	// 也可以直接存储百分比，修改 check.go 中的 showProgress
}
