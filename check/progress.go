package check

import (
	"math"
	"sync/atomic"
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
	speedQualified atomic.Int32 // 测速达标数量

	// 当前处于 测活-测速-媒体检测 阶段 (0=存活, 1=测速, 2=媒体)
	currentStage atomic.Int32
	
	// 当前阶段名称
	stageName atomic.Value // string

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
		pt.speedQualified.Add(1) // 测速达标
	}
	pt.refresh()
}

// CountMedia 标记一个媒体检测已完成，并更新进度。
func (pt *ProgressTracker) CountMedia() {
	pt.mediaDone.Add(1)
	pt.refresh()
}

// GetStageInfo 获取当前阶段信息
func (pt *ProgressTracker) GetStageInfo() (stage int32, name string, done, success int32) {
	stage = pt.currentStage.Load()
	if v := pt.stageName.Load(); v != nil {
		name = v.(string)
	}
	
	switch stage {
	case 0: // 存活检测
		done = pt.aliveDone.Load()
		success = pt.aliveSuccess.Load()
	case 1: // 测速
		done = pt.speedDone.Load()
		success = pt.speedQualified.Load() // 返回测速达标数量而不是测速成功数量
	case 2: // 媒体检测
		done = pt.mediaDone.Load()
		success = 0 // 媒体检测不区分成功失败
	}
	return
}

// refresh 更新进度条显示
func (pt *ProgressTracker) refresh() {
	if pt.finalized.Load() {
		return
	}

	switch progressAlgorithm {
	case DynamicWeightProgress:
		pt.refreshDynamicWeight()
	case StagePriorityProgress:
		pt.refreshStagePriority()
	}
}

// refreshDynamicWeight 动态权重模式
func (pt *ProgressTracker) refreshDynamicWeight() {
	total := pt.totalJobs.Load()
	if total == 0 {
		return
	}

	aliveDone := pt.aliveDone.Load()
	speedDone := pt.speedDone.Load()
	mediaDone := pt.mediaDone.Load()

	aliveWeight := getCheckWeight(true, true).alive
	speedWeight := getCheckWeight(true, true).speed
	mediaWeight := getCheckWeight(true, true).media

	aliveProgress := float64(aliveDone) / float64(total) * aliveWeight
	speedProgress := float64(speedDone) / float64(total) * speedWeight
	mediaProgress := float64(mediaDone) / float64(total) * mediaWeight

	progress := aliveProgress + speedProgress + mediaProgress
	Progress.Store(int32(progress))
}

// refreshStagePriority 阶段优先模式
func (pt *ProgressTracker) refreshStagePriority() {
	total := pt.totalJobs.Load()
	if total == 0 {
		return
	}

	stage := pt.currentStage.Load()
	done := int32(0)
	switch stage {
	case 0: // 存活检测
		done = pt.aliveDone.Load()
	case 1: // 测速
		done = pt.speedDone.Load()
	case 2: // 媒体检测
		done = pt.mediaDone.Load()
	}

	progress := float64(done) / float64(total) * 100
	Progress.Store(int32(progress))
}

// Finalize 结束进度条显示
func (pt *ProgressTracker) Finalize() {
	pt.finalized.Store(true)
}
