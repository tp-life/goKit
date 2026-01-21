package storage

type Config struct {
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
	Bucket    string `mapstructure:"bucket"`
	Domain    string `mapstructure:"domain"` // CDN 域名，用于生成完整 URL
	Zone      string `mapstructure:"zone"`   // 区域：z0(华东), z1(华北), z2(华南), na0(北美), as0(东南亚)
	UseHTTPS  bool   `mapstructure:"use_https"`
}
