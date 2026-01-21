package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/qiniu/go-sdk/v7/auth/qbox"
	"github.com/qiniu/go-sdk/v7/storage"
)

type QiniuStorage struct {
	config     Config
	mac        *qbox.Mac
	uploader   *storage.FormUploader
	logger     *slog.Logger
	publicURL  string
}

func NewQiniuStorage(cfg Config, logger *slog.Logger) (*QiniuStorage, error) {
	mac := qbox.NewMac(cfg.AccessKey, cfg.SecretKey)

	// 解析区域配置
	var zone *storage.Zone
	switch cfg.Zone {
	case "z0", "huadong":
		zone = &storage.ZoneHuadong
	case "z1", "huabei":
		zone = &storage.ZoneHuabei
	case "z2", "huanan":
		zone = &storage.ZoneHuanan
	case "na0", "beimei":
		zone = &storage.ZoneBeimei
	case "as0", "dongnanya":
		zone = &storage.ZoneXinjiapo
	default:
		zone = &storage.ZoneHuanan // 默认华南
	}

	uploadConfig := storage.Config{
		Zone:          zone,
		UseHTTPS:      cfg.UseHTTPS,
		UseCdnDomains: false,
	}

	uploader := storage.NewFormUploader(&uploadConfig)

	// 构建公开访问 URL
	scheme := "http"
	if cfg.UseHTTPS {
		scheme = "https"
	}
	publicURL := fmt.Sprintf("%s://%s", scheme, cfg.Domain)

	return &QiniuStorage{
		config:    cfg,
		mac:       mac,
		uploader:  uploader,
		logger:    logger,
		publicURL: publicURL,
	}, nil
}

// UploadFile 上传文件到七牛云
// key: 文件在七牛云上的路径/名称，例如 "images/2024/01/abc.jpg"
// reader: 文件内容
// size: 文件大小（字节）
// 返回: 完整的公开访问 URL
func (s *QiniuStorage) UploadFile(ctx context.Context, key string, reader io.Reader, size int64) (string, error) {
	// 生成上传凭证
	putPolicy := storage.PutPolicy{
		Scope: s.config.Bucket,
	}
	putPolicy.Expires = 3600 // 1小时过期
	upToken := putPolicy.UploadToken(s.mac)

	// 执行上传
	ret := storage.PutRet{}
	putExtra := storage.PutExtra{}

	err := s.uploader.Put(ctx, &ret, upToken, key, reader, size, &putExtra)
	if err != nil {
		s.logger.ErrorContext(ctx, "qiniu_upload_failed",
			slog.String("key", key),
			slog.Any("error", err),
		)
		return "", fmt.Errorf("upload to qiniu failed: %w", err)
	}

	// 生成完整 URL
	url := fmt.Sprintf("%s/%s", s.publicURL, ret.Key)

	s.logger.InfoContext(ctx, "qiniu_upload_success",
		slog.String("key", ret.Key),
		slog.String("url", url),
	)

	return url, nil
}

// GenerateKey 生成文件存储路径
// 格式: images/YYYY/MM/dd/{timestamp}_{random}.{ext}
func (s *QiniuStorage) GenerateKey(filename string) string {
	now := time.Now()
	return fmt.Sprintf("images/%04d/%02d/%02d/%d_%s",
		now.Year(),
		now.Month(),
		now.Day(),
		now.Unix(),
		filename,
	)
}
