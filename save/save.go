package save

import (
	"fmt"
	"log/slog"

	"github.com/55gY/subs-check/check"
	"github.com/55gY/subs-check/save/method"
	"gopkg.in/yaml.v3"
)

// ConfigSaver 处理配置保存的结构体
type ConfigSaver struct {
	results    []check.Result
	saveMethod func([]byte, string) error
}

// NewConfigSaver 创建新的配置保存器
func NewConfigSaver(results []check.Result) *ConfigSaver {
	return &ConfigSaver{
		results:    results,
		saveMethod: method.SaveToLocal,
	}
}

// SaveConfig 保存配置的入口函数
func SaveConfig(results []check.Result) {
	saver := NewConfigSaver(results)
	if err := saver.Save(); err != nil {
		slog.Error(fmt.Sprintf("保存配置失败: %v", err))
	}
}

// Save 执行保存操作
func (cs *ConfigSaver) Save() error {
	proxies := make([]map[string]any, 0)
	for _, result := range cs.results {
		proxies = append(proxies, result.Proxy)
	}

	if len(proxies) == 0 {
		slog.Warn("yaml节点为空，跳过保存")
		return nil
	}

	yamlData, err := yaml.Marshal(map[string]any{
		"proxies": proxies,
	})
	if err != nil {
		return fmt.Errorf("序列化yaml失败: %w", err)
	}

	if err := cs.saveMethod(yamlData, "all.yaml"); err != nil {
		return fmt.Errorf("保存all.yaml失败: %w", err)
	}

	return nil
}
