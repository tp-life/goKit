package repository

import (
	"context"
	"io"
)

// StorageService 存储服务接口
type StorageService interface {
	UploadFile(ctx context.Context, key string, reader io.Reader, size int64) (string, error)
	GenerateKey(filename string) string
}
