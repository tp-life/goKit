// pkg/kit/log/config.go
package log

type Config struct {
	Level  string `mapstructure:"level" json:"level" yaml:"level"`    // debug, info, warn, error
	Format string `mapstructure:"format" json:"format" yaml:"format"` // json, text
	Source bool   `mapstructure:"source" json:"source" yaml:"source"` // 是否打印文件行号 (生产环境建议关闭提升性能)
}

func DefaultConfig() Config {
	return Config{
		Level:  "info",
		Format: "json",
		Source: false,
	}
}
