package handler

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"goKit/internal/application/dto"
	service "goKit/internal/application/service"
	infraPolymarket "goKit/internal/infrastructure/polymarket"
	"goKit/internal/interface/http/response"

	"github.com/gofiber/fiber/v2"
)

// PolymarketHandler 负责暴露 dashboard、SSE 与手动下单接口。
type PolymarketHandler struct {
	svc *service.PolymarketService
	cfg infraPolymarket.Config
}

// NewPolymarketHandler 创建 Polymarket HTTP 处理器。
func NewPolymarketHandler(svc *service.PolymarketService, cfg infraPolymarket.Config) *PolymarketHandler {
	return &PolymarketHandler{svc: svc, cfg: cfg}
}

// Dashboard 返回静态 dashboard 页面。
func (h *PolymarketHandler) Dashboard(c *fiber.Ctx) error {
	indexPath := filepath.Join(h.cfg.DashboardStaticDir, "dashboard.html")
	if _, err := os.Stat(indexPath); err != nil {
		return response.ErrNotFound("dashboard.html 不存在")
	}
	return c.SendFile(indexPath)
}

// Status 返回当前 dashboard 快照。
func (h *PolymarketHandler) Status(c *fiber.Ctx) error {
	return c.JSON(h.svc.Snapshot())
}

// Logs 返回当前活动日志列表。
func (h *PolymarketHandler) Logs(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"items": h.svc.Logs()})
}

// History 返回当前交易历史列表。
func (h *PolymarketHandler) History(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"items": h.svc.History()})
}

// ManualOrder 处理 dashboard 发起的手动下单请求。
func (h *PolymarketHandler) ManualOrder(c *fiber.Ctx) error {
	var req dto.ManualOrderReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON 解析失败")
	}
	resp, err := h.svc.SubmitManualOrder(c.UserContext(), req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(dto.ManualOrderResp{
			OK:    false,
			Error: err.Error(),
		})
	}
	return c.JSON(resp)
}

// Stream 以 SSE 形式持续推送状态与日志更新。
func (h *PolymarketHandler) Stream(c *fiber.Ctx) error {
	subID, ch := h.svc.Subscribe()
	defer h.svc.Unsubscribe(subID)

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ping := time.NewTicker(15 * time.Second)
		defer ping.Stop()

		for {
			select {
			case snapshot, ok := <-ch:
				if !ok {
					return
				}
				statusPayload := snapshot
				logs := statusPayload.Activity
				statusPayload.Activity = nil
				if err := writeSSEEvent(w, "status", fiber.Map{"data": statusPayload}); err != nil {
					return
				}
				if err := writeSSEEvent(w, "logs", fiber.Map{"items": logs}); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			case <-ping.C:
				if _, err := w.WriteString(": ping\n\n"); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	})
	return nil
}

// writeSSEEvent 按 SSE 协议格式写出一条事件消息。
func writeSSEEvent(w *bufio.Writer, name string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := w.WriteString("event: " + name + "\n"); err != nil {
		return err
	}
	if _, err := w.WriteString("data: " + string(body) + "\n\n"); err != nil {
		return err
	}
	return nil
}
