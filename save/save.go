package save

import (
	"fmt"
	"log/slog"

	"github.com/55gY/subs-check/check"
	"github.com/55gY/subs-check/save/method"
	"gopkg.in/yaml.v3"
)

// ProxyCategory 定义代理分类
type ProxyCategory struct {
	Name    string
	Proxies []map[string]any
	Filter  func(result check.Result) bool
}

// ConfigSaver 处理配置保存的结构体
type ConfigSaver struct {
	results    []check.Result
	categories []ProxyCategory
	saveMethod func([]byte, string) error
}

// NewConfigSaver 创建新的配置保存器
func NewConfigSaver(results []check.Result) *ConfigSaver {
	return &ConfigSaver{
		results:    results,
		saveMethod: chooseSaveMethod(),
		categories: []ProxyCategory{
			{
				Name:    "all.yaml",
				Proxies: make([]map[string]any, 0),
				Filter:  func(result check.Result) bool { return true },
			},
		},
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
	// 分类处理代理
	cs.categorizeProxies()

	// 保存各个类别的代理
	for _, category := range cs.categories {
		if err := cs.saveCategory(category); err != nil {
			slog.Error(fmt.Sprintf("保存失败: %v", err))
			continue
		}
	}

	return nil
}

// categorizeProxies 将代理按类别分类
func (cs *ConfigSaver) categorizeProxies() {
	for _, result := range cs.results {
		for i := range cs.categories {
			if cs.categories[i].Filter(result) {
				cs.categories[i].Proxies = append(cs.categories[i].Proxies, result.Proxy)
			}
		}
	}
}

// saveCategory 保存单个类别的代理
func (cs *ConfigSaver) saveCategory(category ProxyCategory) error {
	if len(category.Proxies) == 0 {
		slog.Warn(fmt.Sprintf("yaml节点为空，跳过保存: %s", category.Name))
		return nil
	}

	yamlData, err := yaml.Marshal(map[string]any{
		"proxies": category.Proxies,
	})
	if err != nil {
		return fmt.Errorf("序列化yaml %s 失败: %w", category.Name, err)
	}
	if err := cs.saveMethod(yamlData, category.Name); err != nil {
		return fmt.Errorf("保存 %s 失败: %w", category.Name, err)
	}
	return nil
}

// chooseSaveMethod 根据配置选择保存方法
func chooseSaveMethod() func([]byte, string) error {
	return method.SaveToLocal
}
