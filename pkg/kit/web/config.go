package web

type Config struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    string `mapstructure:"port"`
	AppName string `mapstructure:"app_name"`
	Prefork bool   `mapstructure:"prefork"`
}
