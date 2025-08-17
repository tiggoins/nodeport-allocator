package config

// Config 主配置结构
type Config struct {
    PortRanges              map[string]PortRange `yaml:"portRanges"`
    DefaultRange            string               `yaml:"defaultRange"`
    AllowOutsideRangePorts  bool                 `yaml:"allowOutsideRangePorts"`
    StorageConfig           StorageConfig        `yaml:"storage"`
    LogLevel                string               `yaml:"logLevel"`
}

// PortRange 端口范围配置
type PortRange struct {
    Start       int32              `yaml:"start"`
    End         int32              `yaml:"end"`
    Namespaces  []string           `yaml:"namespaces"`
    Labels      map[string]string  `yaml:"labels"`
    Description string             `yaml:"description"`
}

// StorageConfig 存储配置
type StorageConfig struct {
    ConfigMapName      string `yaml:"configMapName"`
    ConfigMapNamespace string `yaml:"configMapNamespace"`
    RetryAttempts      int    `yaml:"retryAttempts"`
    RetryDelay         string `yaml:"retryDelay"`
}
