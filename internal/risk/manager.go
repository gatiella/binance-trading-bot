// File: internal/risk/manager.go
// ============================================
package risk

import (
    "binance-trading-bot/pkg/types"
    "fmt"
)

type Manager struct {
    config         *types.Config
    dailyPnL       float64
    initialBalance float64
}

func NewManager(config *types.Config, initialBalance float64) *Manager {
    return &Manager{
        config:         config,
        dailyPnL:       0,
        initialBalance: initialBalance,
    }
}

func (m *Manager) CanOpenPosition(positions []types.Position) (bool, string) {
    if len(positions) >= m.config.Strategy.MaxPositions {
        return false, "Maximum positions reached"
    }
    
    if m.dailyPnL < -m.config.Risk.MaxDailyLoss {
        return false, fmt.Sprintf("Daily loss limit reached: %.2f USDT", m.dailyPnL)
    }
    
    return true, ""
}

func (m *Manager) CalculatePositionSize(price float64) float64 {
    quantity := m.config.Strategy.PositionSize / price
    return quantity
}

func (m *Manager) CalculateStopLoss(entryPrice float64, side string) float64 {
    stopLossPercent := m.config.Strategy.StopLossPercent / 100.0
    
    if side == "BUY" {
        return entryPrice * (1 - stopLossPercent)
    }
    return entryPrice * (1 + stopLossPercent)
}

func (m *Manager) CalculateTakeProfit(entryPrice float64, side string) float64 {
    takeProfitPercent := m.config.Strategy.TakeProfitPercent / 100.0
    
    if side == "BUY" {
        return entryPrice * (1 + takeProfitPercent)
    }
    return entryPrice * (1 - takeProfitPercent)
}

func (m *Manager) UpdateTrailingStop(position *types.Position) bool {
    if !m.config.Strategy.TrailingStopEnabled || !position.TrailingStopEnabled {
        return false
    }
    
    // Update highest price
    if position.CurrentPrice > position.HighestPrice {
        position.HighestPrice = position.CurrentPrice
        
        // Calculate new trailing stop
        trailingPercent := m.config.Strategy.TrailingStopPercent / 100.0
        newTrailingStop := position.HighestPrice * (1 - trailingPercent)
        
        // Only update if new stop is higher than current
        if newTrailingStop > position.TrailingStopPrice {
            position.TrailingStopPrice = newTrailingStop
            return true // Trailing stop was updated
        }
    }
    
    return false
}

func (m *Manager) ShouldClosePosition(position types.Position) (bool, string) {
    // Check trailing stop first
    if position.TrailingStopEnabled && position.CurrentPrice <= position.TrailingStopPrice {
        return true, fmt.Sprintf("Trailing stop hit at $%.4f", position.TrailingStopPrice)
    }
    
    // Check regular stop loss
    if position.Side == "BUY" && position.CurrentPrice <= position.StopLoss {
        return true, "Stop loss hit"
    }
    
    // Check take profit
    if position.Side == "BUY" && position.CurrentPrice >= position.TakeProfit {
        return true, "Take profit hit"
    }
    
    return false, ""
}

func (m *Manager) UpdateDailyPnL(pnl float64) {
    m.dailyPnL += pnl
}

func (m *Manager) GetDailyPnL() float64 {
    return m.dailyPnL
}

func (m *Manager) ResetDailyPnL() {
    m.dailyPnL = 0
}
