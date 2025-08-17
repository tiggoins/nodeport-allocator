package config

import (
    "fmt"
    "os"
    "time"

    "gopkg.in/yaml.v2"
)

// LoadConfig 从文件加载配置
func LoadConfig(configFile string) (*Config, error) {
    data, err := os.ReadFile(configFile)
    if err != nil {
        return nil, fmt.Errorf("读取配置文件失败: %v", err)
    }

    var config Config
    if err := yaml.Unmarshal(data, &config); err != nil {
        return nil, fmt.Errorf("解析配置文件失败: %v", err)
    }

    // 设置默认值
    if config.StorageConfig.ConfigMapName == "" {
        config.StorageConfig.ConfigMapName = "nodeport-allocator-state"
    }
    if config.StorageConfig.ConfigMapNamespace == "" {
        config.StorageConfig.ConfigMapNamespace = "kube-system"
    }
    if config.StorageConfig.RetryAttempts == 0 {
        config.StorageConfig.RetryAttempts = 3
    }
    if config.StorageConfig.RetryDelay == "" {
        config.StorageConfig.RetryDelay = "1s"
    }
    if config.LogLevel == "" {
        config.LogLevel = "info"
    }

    // 验证配置
    if err := validateConfig(&config); err != nil {
        return nil, fmt.Errorf("配置验证失败: %v", err)
    }

    return &config, nil
}

// validateConfig 验证配置的合法性
func validateConfig(config *Config) error {
    if len(config.PortRanges) == 0 {
        return fmt.Errorf("至少需要配置一个端口范围")
    }

    if config.DefaultRange == "" {
        return fmt.Errorf("必须指定默认端口范围")
    }

    if _, exists := config.PortRanges[config.DefaultRange]; !exists {
        return fmt.Errorf("默认端口范围 %s 不存在", config.DefaultRange)
    }

    for name, portRange := range config.PortRanges {
        if portRange.Start <= 0 || portRange.End <= 0 {
            return fmt.Errorf("端口范围 %s 的起始或结束端口无效", name)
        }
        if portRange.Start >= portRange.End {
            return fmt.Errorf("端口范围 %s 的起始端口必须小于结束端口", name)
        }
        if portRange.Start < 30000 || portRange.End > 32767 {
            return fmt.Errorf("端口范围 %s 超出 NodePort 允许范围 (30000-32767)", name)
        }
    }

    // 验证重试延迟格式
    if _, err := time.ParseDuration(config.StorageConfig.RetryDelay); err != nil {
        return fmt.Errorf("重试延迟格式无效: %v", err)
    }

    return nil
}

// GetPortRangeForNamespace 获取指定命名空间的端口范围
func (c *Config) GetPortRangeForNamespace(namespace string) (string, PortRange, error) {
    // 查找匹配的端口范围
    for rangeName, portRange := range c.PortRanges {
        for _, ns := range portRange.Namespaces {
            if ns == namespace || ns == "*" {
                return rangeName, portRange, nil
            }
        }
    }

    // 返回默认范围
    defaultRange, exists := c.PortRanges[c.DefaultRange]
    if !exists {
        return "", PortRange{}, fmt.Errorf("默认端口范围 %s 不存在", c.DefaultRange)
    }

    return c.DefaultRange, defaultRange, nil
}

// GetPortRangeForService 获取指定Service的端口范围
func (c *Config) GetPortRangeForService(namespace string, labels map[string]string) (string, PortRange, error) {
    // 首先尝试基于标签匹配
    for rangeName, portRange := range c.PortRanges {
        // 检查标签匹配
        if len(portRange.Labels) > 0 {
            match := true
            for key, value := range portRange.Labels {
                if serviceValue, exists := labels[key]; !exists || serviceValue != value {
                    match = false
                    break
                }
            }
            if match {
                return rangeName, portRange, nil
            }
        }
    }

    // 如果没有标签匹配，回退到基于namespace的匹配
    return c.GetPortRangeForNamespace(namespace)
}
