package checker

import (
	"math"
	"sync/atomic"
	"time"
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

	// 当前处于 测活-测速-媒体检测 阶段 (0=存活, 1=测速, 2=媒体)
	currentStage atomic.Int32

	// 当前阶段名称
	stageName atomic.Value // string

	// 确保进度条输出完成
	finalized atomic.Bool

	// 超时倒计时相关
	timeoutStartTime atomic.Value // time.Time
	timeoutDuration  atomic.Value // time.Duration
}

// NewProgressTracker 初始化进度追踪器并重置外部原子变量。
func NewProgressTracker(total int) *ProgressTracker {
	pt := &ProgressTracker{}
	if total > math.MaxInt32 {
		total = math.MaxInt32
	}
	pt.totalJobs.Store(int32(total))
	pt.currentStage.Store(0)
	pt.stageName.Store("存活检测")

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

// GetStats 获取当前统计信息（供API使用）
func (pt *ProgressTracker) GetStats() (totalNodes, aliveSuccess, aliveDone, speedSuccess, speedDone, mediaDone int32) {
	return pt.totalJobs.Load(),
		pt.aliveSuccess.Load(),
		pt.aliveDone.Load(),
		pt.speedSuccess.Load(),
		pt.speedDone.Load(),
		pt.mediaDone.Load()
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

	// Progress 直接存储整体加权进度百分比（放大100倍，保留两位小数）
	Progress.Store(uint32(currentProgress * 100))
}

// SetStage 设置当前阶段
func (pt *ProgressTracker) SetStage(stage int32, name string) {
	pt.currentStage.Store(stage)
	pt.stageName.Store(name)
}

// SetTimeout 设置超时信息
func (pt *ProgressTracker) SetTimeout(duration time.Duration) {
	pt.timeoutStartTime.Store(time.Now())
	pt.timeoutDuration.Store(duration)
}

// GetTimeoutRemaining 获取剩余超时时间（秒）
func (pt *ProgressTracker) GetTimeoutRemaining() int {
	startTime, ok1 := pt.timeoutStartTime.Load().(time.Time)
	duration, ok2 := pt.timeoutDuration.Load().(time.Duration)
	if !ok1 || !ok2 {
		return 0
	}
	elapsed := time.Since(startTime)
	remaining := duration - elapsed
	if remaining < 0 {
		return 0
	}
	return int(remaining.Seconds())
}

// ClearTimeout 清除超时信息
func (pt *ProgressTracker) ClearTimeout() {
	pt.timeoutStartTime.Store(time.Time{})
	pt.timeoutDuration.Store(time.Duration(0))
}

// GetStageInfo 获取当前阶段信息
func (pt *ProgressTracker) GetStageInfo() (stage int32, name string, done, success, total int32) {
	stage = pt.currentStage.Load()
	if v := pt.stageName.Load(); v != nil {
		name = v.(string)
	}

	switch stage {
	case 0: // 存活检测
		done = pt.aliveDone.Load()
		success = pt.aliveSuccess.Load()
		total = pt.totalJobs.Load()
	case 1: // 测速
		done = pt.speedDone.Load()
		success = pt.speedSuccess.Load()
		total = pt.aliveSuccess.Load()
	case 2: // 媒体检测
		done = pt.mediaDone.Load()
		success = pt.mediaDone.Load() // 媒体检测不区分成功失败，完成即视为成功
		total = pt.speedSuccess.Load()
	}
	return
}
