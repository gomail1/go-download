package utils

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

// 监控告警工具

// AlertLevel 告警级别
type AlertLevel string

const (
	AlertLevelInfo     AlertLevel = "info"
	AlertLevelWarning  AlertLevel = "warning"
	AlertLevelCritical AlertLevel = "critical"
)

// AlertType 告警类型
type AlertType string

const (
	AlertTypeCPU         AlertType = "cpu"
	AlertTypeMemory      AlertType = "memory"
	AlertTypeDisk        AlertType = "disk"
	AlertTypeRequestRate AlertType = "request_rate"
	AlertTypeErrorRate   AlertType = "error_rate"
	AlertTypeResponseTime AlertType = "response_time"
	AlertTypeSecurity    AlertType = "security"
)

// Alert 告警信息
type Alert struct {
	ID        string
	Level     AlertLevel
	Type      AlertType
	Message   string
	Value     float64
	Threshold float64
	Timestamp time.Time
	Resolved  bool
}

// SystemMetrics 系统资源指标
type SystemMetrics struct {
	CPUUsage     float64 // CPU使用率（%）
	MemoryUsage  float64 // 内存使用率（%）
	DiskUsage    float64 // 磁盘使用率（%）
	GoroutineNum int     // goroutine数量
	AllocMem     uint64  // 已分配内存（字节）
	SysMem       uint64  // 系统内存（字节）
}

// AppMetrics 应用性能指标
type AppMetrics struct {
	TotalRequests   int64         // 总请求数
	SuccessRequests int64         // 成功请求数
	ErrorRequests   int64         // 错误请求数
	TotalResponseTime time.Duration // 总响应时间
	AvgResponseTime time.Duration   // 平均响应时间
	MaxResponseTime time.Duration   // 最大响应时间
	RequestsPerMinute float64       // 每分钟请求数
	ErrorRate        float64        // 错误率（%）
}

// AlertRule 告警规则
type AlertRule struct {
	Type      AlertType
	Level     AlertLevel
	Threshold float64
	Duration  time.Duration
	Message   string
	Enabled   bool
}

// Monitor 监控器
type Monitor struct {
	systemMetrics SystemMetrics
	appMetrics    AppMetrics
	alerts        []*Alert
	alertRules    []*AlertRule
	mu            sync.RWMutex
	stopChan      chan struct{}
}

var (
	globalMonitor *Monitor
	monitorOnce   sync.Once
)

// GetMonitor 获取全局监控器实例
func GetMonitor() *Monitor {
	monitorOnce.Do(func() {
		globalMonitor = &Monitor{
			stopChan: make(chan struct{}),
			alertRules: []*AlertRule{
				{Type: AlertTypeCPU, Level: AlertLevelWarning, Threshold: 70, Duration: 5 * time.Minute, Message: "CPU使用率超过70%%", Enabled: true},
				{Type: AlertTypeCPU, Level: AlertLevelCritical, Threshold: 90, Duration: 1 * time.Minute, Message: "CPU使用率超过90%%", Enabled: true},
				{Type: AlertTypeMemory, Level: AlertLevelWarning, Threshold: 70, Duration: 5 * time.Minute, Message: "内存使用率超过70%%", Enabled: true},
				{Type: AlertTypeMemory, Level: AlertLevelCritical, Threshold: 90, Duration: 1 * time.Minute, Message: "内存使用率超过90%%", Enabled: true},
				{Type: AlertTypeDisk, Level: AlertLevelWarning, Threshold: 80, Duration: 1 * time.Hour, Message: "磁盘使用率超过80%%", Enabled: true},
				{Type: AlertTypeDisk, Level: AlertLevelCritical, Threshold: 95, Duration: 10 * time.Minute, Message: "磁盘使用率超过95%%", Enabled: true},
				{Type: AlertTypeErrorRate, Level: AlertLevelWarning, Threshold: 5, Duration: 5 * time.Minute, Message: "错误率超过5%%", Enabled: true},
				{Type: AlertTypeErrorRate, Level: AlertLevelCritical, Threshold: 20, Duration: 1 * time.Minute, Message: "错误率超过20%%", Enabled: true},
			},
		}
	})
	return globalMonitor
}

// Start 启动监控
func (m *Monitor) Start() {
	// 启动系统资源采集
	go m.collectSystemMetrics()

	// 启动告警检查
	go m.checkAlerts()
}

// Stop 停止监控
func (m *Monitor) Stop() {
	close(m.stopChan)
}

// collectSystemMetrics 采集系统资源指标
func (m *Monitor) collectSystemMetrics() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.mu.Lock()

			// 采集内存指标
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			m.systemMetrics.AllocMem = memStats.Alloc
			m.systemMetrics.SysMem = memStats.Sys
			m.systemMetrics.GoroutineNum = runtime.NumGoroutine()

			// 注意：Go标准库没有直接获取CPU和磁盘使用率的方法
			// 这里使用简化的估算，实际生产环境建议使用第三方库如gopsutil
			m.systemMetrics.CPUUsage = m.estimateCPUUsage()
			m.systemMetrics.MemoryUsage = m.estimateMemoryUsage()
			m.systemMetrics.DiskUsage = m.estimateDiskUsage()

			m.mu.Unlock()
		case <-m.stopChan:
			return
		}
	}
}

// estimateCPUUsage 估算CPU使用率（简化实现）
func (m *Monitor) estimateCPUUsage() float64 {
	// 简化实现：基于goroutine数量估算
	// 实际生产环境建议使用gopsutil等库
	goroutineNum := runtime.NumGoroutine()
	cpuUsage := float64(goroutineNum) / 100 * 100
	if cpuUsage > 100 {
		cpuUsage = 100
	}
	return cpuUsage
}

// estimateMemoryUsage 估算内存使用率（简化实现）
func (m *Monitor) estimateMemoryUsage() float64 {
	// 简化实现：基于已分配内存估算
	// 实际生产环境建议使用gopsutil等库
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// 假设系统内存为4GB（简化实现）
	const systemMemory = 4 * 1024 * 1024 * 1024
	memoryUsage := float64(memStats.Alloc) / float64(systemMemory) * 100
	if memoryUsage > 100 {
		memoryUsage = 100
	}
	return memoryUsage
}

// estimateDiskUsage 估算磁盘使用率（跨平台实现，具体实现在平台特定文件中）
func (m *Monitor) estimateDiskUsage() float64 {
	return getDiskUsage()
}

// RecordRequest 记录请求
func (m *Monitor) RecordRequest(success bool, responseTime time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.appMetrics.TotalRequests++
	if success {
		m.appMetrics.SuccessRequests++
	} else {
		m.appMetrics.ErrorRequests++
	}
	m.appMetrics.TotalResponseTime += responseTime

	// 更新最大响应时间
	if responseTime > m.appMetrics.MaxResponseTime {
		m.appMetrics.MaxResponseTime = responseTime
	}

	// 更新平均响应时间
	if m.appMetrics.TotalRequests > 0 {
		m.appMetrics.AvgResponseTime = m.appMetrics.TotalResponseTime / time.Duration(m.appMetrics.TotalRequests)
	}

	// 更新错误率
	if m.appMetrics.TotalRequests > 0 {
		m.appMetrics.ErrorRate = float64(m.appMetrics.ErrorRequests) / float64(m.appMetrics.TotalRequests) * 100
	}
}

// checkAlerts 检查告警规则
func (m *Monitor) checkAlerts() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.mu.RLock()
			metrics := m.systemMetrics
			appMetrics := m.appMetrics
			rules := m.alertRules
			m.mu.RUnlock()

			for _, rule := range rules {
				if !rule.Enabled {
					continue
				}

				var value float64
				switch rule.Type {
				case AlertTypeCPU:
					value = metrics.CPUUsage
				case AlertTypeMemory:
					value = metrics.MemoryUsage
				case AlertTypeDisk:
					value = metrics.DiskUsage
				case AlertTypeErrorRate:
					value = appMetrics.ErrorRate
				}

				if value >= rule.Threshold {
					m.triggerAlert(rule, value)
				}
			}
		case <-m.stopChan:
			return
		}
	}
}

// triggerAlert 触发告警
func (m *Monitor) triggerAlert(rule *AlertRule, value float64) {
	alert := &Alert{
		ID:        fmt.Sprintf("alert_%d", time.Now().UnixNano()),
		Level:     rule.Level,
		Type:      rule.Type,
		Message:   rule.Message,
		Value:     value,
		Threshold: rule.Threshold,
		Timestamp: time.Now(),
		Resolved:  false,
	}

	m.mu.Lock()
	m.alerts = append(m.alerts, alert)
	// 只保留最近1000条告警
	if len(m.alerts) > 1000 {
		m.alerts = m.alerts[len(m.alerts)-1000:]
	}
	m.mu.Unlock()

	// 记录告警日志
	Log(LogLevelWarning, "system", "monitor", "alert_triggered",
		fmt.Sprintf("[%s] %s (当前值: %.2f, 阈值: %.2f)", alert.Level, alert.Message, value, rule.Threshold))
}

// GetSystemMetrics 获取系统资源指标
func (m *Monitor) GetSystemMetrics() SystemMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.systemMetrics
}

// GetAppMetrics 获取应用性能指标
func (m *Monitor) GetAppMetrics() AppMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.appMetrics
}

// GetAlerts 获取告警列表
func (m *Monitor) GetAlerts(limit int) []*Alert {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 || limit > len(m.alerts) {
		limit = len(m.alerts)
	}

	// 返回最近的告警
	result := make([]*Alert, limit)
	copy(result, m.alerts[len(m.alerts)-limit:])
	return result
}

// GetActiveAlerts 获取未解决的告警
func (m *Monitor) GetActiveAlerts() []*Alert {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var activeAlerts []*Alert
	for _, alert := range m.alerts {
		if !alert.Resolved {
			activeAlerts = append(activeAlerts, alert)
		}
	}
	return activeAlerts
}

// ResolveAlert 解决告警
func (m *Monitor) ResolveAlert(alertID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, alert := range m.alerts {
		if alert.ID == alertID {
			alert.Resolved = true
			return true
		}
	}
	return false
}

// AddAlertRule 添加告警规则
func (m *Monitor) AddAlertRule(rule *AlertRule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alertRules = append(m.alertRules, rule)
}

// GetAlertRules 获取告警规则
func (m *Monitor) GetAlertRules() []*AlertRule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.alertRules
}

// ResetMetrics 重置指标
func (m *Monitor) ResetMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appMetrics = AppMetrics{}
}
