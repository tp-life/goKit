package persistence

import (
	"context"
	"errors"
	"strings"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"

	"gorm.io/gorm"
)

type OrderRepo struct {
	client *db.Client
}

func NewOrderRepository(client *db.Client) repository.OrderRepository {
	return &OrderRepo{client: client}
}

func (r *OrderRepo) Create(ctx context.Context, item *entity.OrderRecord) error {
	return r.client.GetDB(ctx).Create(item).Error
}

func (r *OrderRepo) Update(ctx context.Context, item *entity.OrderRecord) error {
	return r.client.GetDB(ctx).Save(item).Error
}

func (r *OrderRepo) FindByExternalOrderID(ctx context.Context, exchangeName, clientOrderID, venueOrderID string) (*entity.OrderRecord, error) {
	dbh := r.client.GetDB(ctx)
	query := dbh.Model(&entity.OrderRecord{}).Where("exchange = ?", strings.ToLower(strings.TrimSpace(exchangeName)))

	switch {
	case strings.TrimSpace(clientOrderID) != "" && strings.TrimSpace(venueOrderID) != "":
		query = query.Where("(client_order_id = ? OR venue_order_id = ?)", clientOrderID, venueOrderID)
	case strings.TrimSpace(clientOrderID) != "":
		query = query.Where("client_order_id = ?", clientOrderID)
	case strings.TrimSpace(venueOrderID) != "":
		query = query.Where("venue_order_id = ?", venueOrderID)
	default:
		return nil, nil
	}

	var out entity.OrderRecord
	err := query.Order("created_at desc").First(&out).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *OrderRepo) ListByPlanKey(ctx context.Context, planKey string) ([]entity.OrderRecord, error) {
	var out []entity.OrderRecord
	err := r.client.GetDB(ctx).Where("plan_key = ?", planKey).Order("created_at asc").Find(&out).Error
	return out, err
}

func (r *OrderRepo) ListLatest(ctx context.Context, limit int) ([]entity.OrderRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	var out []entity.OrderRecord
	err := r.client.GetDB(ctx).Order("created_at desc").Limit(limit).Find(&out).Error
	return out, err
}
