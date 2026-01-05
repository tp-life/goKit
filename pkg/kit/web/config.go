package web

type Config struct {
	Port    string `mapstructure:"port"`
	AppName string `mapstructure:"app_name"`
	Prefork bool   `mapstructure:"prefork"`
}
