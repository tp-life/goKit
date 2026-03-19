package exchange

import "testing"

func TestCloseSideAndQuantity(t *testing.T) {
	tests := []struct {
		name         string
		positionQty  float64
		requestedQty float64
		wantSide     string
		wantQty      float64
		wantOK       bool
	}{
		{name: "zero position", positionQty: 0, requestedQty: 1, wantOK: false},
		{name: "long position capped by requested qty", positionQty: 5, requestedQty: 2, wantSide: "SELL", wantQty: 2, wantOK: true},
		{name: "short position capped by actual qty", positionQty: -3, requestedQty: 10, wantSide: "BUY", wantQty: 3, wantOK: true},
		{name: "requested qty missing uses full position", positionQty: 1.5, requestedQty: 0, wantSide: "SELL", wantQty: 1.5, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSide, gotQty, gotOK := closeSideAndQuantity(tt.positionQty, tt.requestedQty)
			if gotOK != tt.wantOK {
				t.Fatalf("expected ok=%v, got %v", tt.wantOK, gotOK)
			}
			if gotSide != tt.wantSide {
				t.Fatalf("expected side %s, got %s", tt.wantSide, gotSide)
			}
			if gotQty != tt.wantQty {
				t.Fatalf("expected qty %v, got %v", tt.wantQty, gotQty)
			}
		})
	}
}
