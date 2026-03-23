package persistence

import (
	"context"
	"fmt"
	"strings"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
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

	var items []entity.OrderRecord
	err := query.Order("created_at desc").Find(&items).Error
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}

	bestIndex := -1
	bestScore := -1
	ambiguous := false
	for i := range items {
		score, ok := externalOrderMatchScore(items[i], clientOrderID, venueOrderID)
		if !ok {
			continue
		}
		if score > bestScore {
			bestIndex = i
			bestScore = score
			ambiguous = false
			continue
		}
		if score == bestScore {
			ambiguous = true
		}
	}
	if bestIndex < 0 {
		return nil, nil
	}
	if ambiguous {
		return nil, fmt.Errorf("ambiguous external order reference for exchange=%s client_order_id=%s venue_order_id=%s", exchangeName, clientOrderID, venueOrderID)
	}
	return &items[bestIndex], nil
}

func externalOrderMatchScore(order entity.OrderRecord, clientOrderID, venueOrderID string) (int, bool) {
	clientOrderID = strings.TrimSpace(clientOrderID)
	venueOrderID = strings.TrimSpace(venueOrderID)

	score := 0
	if clientOrderID != "" {
		switch {
		case strings.TrimSpace(order.ClientOrderID) == clientOrderID:
			score += 10
		case strings.TrimSpace(order.ClientOrderID) != "":
			return 0, false
		}
	}
	if venueOrderID != "" {
		switch {
		case strings.TrimSpace(order.VenueOrderID) == venueOrderID:
			score += 10
		case strings.TrimSpace(order.VenueOrderID) != "":
			return 0, false
		}
	}
	return score, score > 0
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
