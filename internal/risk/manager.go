// File: internal/risk/manager.go
// ============================================
package risk

import (
    "binance-trading-bot/pkg/types"
    "fmt"
    "math"
    "time"
)

type Manager struct {
    config         *types.Config
    dailyPnL       float64
    initialBalance float64
    tradeHistory   []TradeResult
}

type TradeResult struct {
    Symbol    string
    PnL       float64
    Duration  float64  // in minutes
    Success   bool
}

func NewManager(config *types.Config, initialBalance float64) *Manager {
    return &Manager{
        config:         config,
        dailyPnL:       0,
        initialBalance: initialBalance,
        tradeHistory:   make([]TradeResult, 0),
    }
}

func (m *Manager) CanOpenPosition(positions []types.Position) (bool, string) {
    if len(positions) >= m.config.Strategy.MaxPositions {
        return false, "Maximum positions reached"
    }
    
    if m.dailyPnL < -m.config.Risk.MaxDailyLoss {
        return false, fmt.Sprintf("Daily loss limit reached: %.2f USDT", m.dailyPnL)
    }
    
    // NEW: Check win rate - if losing streak, reduce position size or stop
    if len(m.tradeHistory) >= 5 {
        recentTrades := m.tradeHistory[len(m.tradeHistory)-5:]
        losses := 0
        for _, trade := range recentTrades {
            if !trade.Success {
                losses++
            }
        }
        
        // If 4 out of last 5 trades were losses, pause trading
        if losses >= 4 {
            return false, "High loss rate detected - pausing to protect capital"
        }
    }
    
    return true, ""
}

// NEW: Dynamic position sizing based on signal strength and market conditions
func (m *Manager) CalculatePositionSize(price float64, signalStrength float64, volatility float64) float64 {
    baseSize := m.config.Strategy.PositionSize
    
    // Adjust based on signal strength (0.6-1.0 range)
    // Strong signals (0.9-1.0) = 100% of base size
    // Medium signals (0.7-0.89) = 75% of base size
    // Weak signals (0.6-0.69) = 50% of base size
    strengthMultiplier := 1.0
    if signalStrength >= 0.9 {
        strengthMultiplier = 1.0
    } else if signalStrength >= 0.7 {
        strengthMultiplier = 0.75
    } else {
        strengthMultiplier = 0.5
    }
    
    // Adjust based on volatility
    // High volatility = reduce position size
    // volatility > 5% = 50% of base
    // volatility > 3% = 75% of base
    // volatility <= 3% = 100% of base
    volatilityMultiplier := 1.0
    if volatility > 5.0 {
        volatilityMultiplier = 0.5
    } else if volatility > 3.0 {
        volatilityMultiplier = 0.75
    }
    
    // Adjust based on recent performance
    performanceMultiplier := 1.0
    if len(m.tradeHistory) >= 3 {
        recentTrades := m.tradeHistory[len(m.tradeHistory)-3:]
        wins := 0
        for _, trade := range recentTrades {
            if trade.Success {
                wins++
            }
        }
        
        // If winning, can increase size slightly
        if wins == 3 {
            performanceMultiplier = 1.2  // 20% increase
        } else if wins == 0 {
            performanceMultiplier = 0.6  // 40% decrease
        }
    }
    
    // Calculate final position size
    adjustedSize := baseSize * strengthMultiplier * volatilityMultiplier * performanceMultiplier
    
    // Ensure minimum and maximum bounds
    minSize := baseSize * 0.3  // Never go below 30% of base
    maxSize := baseSize * 1.5  // Never go above 150% of base
    
    if adjustedSize < minSize {
        adjustedSize = minSize
    }
    if adjustedSize > maxSize {
        adjustedSize = maxSize
    }
    
    quantity := adjustedSize / price
    return quantity
}

// Keep backward compatibility
func (m *Manager) CalculatePositionSizeSimple(price float64) float64 {
    return m.config.Strategy.PositionSize / price
}

// NEW: Dynamic stop loss based on ATR (volatility)
func (m *Manager) CalculateStopLoss(entryPrice float64, side string, atr float64) float64 {
    baseStopLossPercent := m.config.Strategy.StopLossPercent / 100.0
    
    // If ATR is available, use it for dynamic stop loss
    if atr > 0 {
        // Use 2x ATR as stop loss distance, but respect min/max bounds
        atrBasedStop := (2.0 * atr) / entryPrice
        
        // Keep stop loss between 1.5% and 4%
        minStop := 0.015
        maxStop := 0.04
        
        if atrBasedStop < minStop {
            atrBasedStop = minStop
        }
        if atrBasedStop > maxStop {
            atrBasedStop = maxStop
        }
        
        if side == "BUY" {
            return entryPrice * (1 - atrBasedStop)
        }
        return entryPrice * (1 + atrBasedStop)
    }
    
    // Fallback to config-based stop loss
    if side == "BUY" {
        return entryPrice * (1 - baseStopLossPercent)
    }
    return entryPrice * (1 + baseStopLossPercent)
}

// Keep backward compatibility
func (m *Manager) CalculateStopLossSimple(entryPrice float64, side string) float64 {
    return m.CalculateStopLoss(entryPrice, side, 0)
}

// NEW: Dynamic take profit with scaling exits
func (m *Manager) CalculateTakeProfit(entryPrice float64, side string, signalStrength float64) float64 {
    baseTakeProfitPercent := m.config.Strategy.TakeProfitPercent / 100.0
    
    // Strong signals can aim for higher targets
    // Signal 0.9-1.0 = 1.5x base target
    // Signal 0.7-0.89 = 1.0x base target
    // Signal 0.6-0.69 = 0.75x base target
    multiplier := 1.0
    if signalStrength >= 0.9 {
        multiplier = 1.5
    } else if signalStrength < 0.7 {
        multiplier = 0.75
    }
    
    adjustedTakeProfit := baseTakeProfitPercent * multiplier
    
    // Cap at reasonable bounds (3% - 10%)
    if adjustedTakeProfit < 0.03 {
        adjustedTakeProfit = 0.03
    }
    if adjustedTakeProfit > 0.10 {
        adjustedTakeProfit = 0.10
    }
    
    if side == "BUY" {
        return entryPrice * (1 + adjustedTakeProfit)
    }
    return entryPrice * (1 - adjustedTakeProfit)
}

// Keep backward compatibility
func (m *Manager) CalculateTakeProfitSimple(entryPrice float64, side string) float64 {
    return m.CalculateTakeProfit(entryPrice, side, 0.7)
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
        
        // NEW: Tighten trailing stop as profit increases
        profitPercent := (position.HighestPrice - position.EntryPrice) / position.EntryPrice
        
        // If profit > 8%, tighten trailing stop to 1%
        // If profit > 5%, tighten trailing stop to 1.25%
        // Otherwise use config value
        if profitPercent > 0.08 {
            trailingPercent = 0.01
        } else if profitPercent > 0.05 {
            trailingPercent = 0.0125
        }
        
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
    
    // NEW: Time-based exit - if position is open for too long and not profitable
    if !position.EntryTime.IsZero() {
        positionAge := time.Since(position.EntryTime)
        
        // If position is open for more than 4 hours and not profitable, close it
        if positionAge > 4*time.Hour && position.PnLPercent < 1.0 {
            return true, fmt.Sprintf("Time-based exit: Position open for %.1f hours with minimal profit", positionAge.Hours())
        }
        
        // If position is open for more than 24 hours, close regardless
        if positionAge > 24*time.Hour {
            return true, fmt.Sprintf("Time-based exit: Position open for %.1f hours", positionAge.Hours())
        }
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

// NEW: Record trade results for performance tracking
func (m *Manager) RecordTrade(symbol string, pnl float64, duration float64) {
    result := TradeResult{
        Symbol:   symbol,
        PnL:      pnl,
        Duration: duration,
        Success:  pnl > 0,
    }
    
    m.tradeHistory = append(m.tradeHistory, result)
    
    // Keep only last 50 trades
    if len(m.tradeHistory) > 50 {
        m.tradeHistory = m.tradeHistory[1:]
    }
}

// NEW: Get win rate statistics
func (m *Manager) GetWinRate() (winRate float64, totalTrades int) {
    if len(m.tradeHistory) == 0 {
        return 0, 0
    }
    
    wins := 0
    for _, trade := range m.tradeHistory {
        if trade.Success {
            wins++
        }
    }
    
    return float64(wins) / float64(len(m.tradeHistory)), len(m.tradeHistory)
}

// NEW: Calculate Kelly Criterion for optimal position sizing
func (m *Manager) CalculateKellyCriterion() float64 {
    if len(m.tradeHistory) < 10 {
        return 0.5 // Default to 50% of capital until enough data
    }
    
    wins := 0
    totalWin := 0.0
    totalLoss := 0.0
    
    for _, trade := range m.tradeHistory {
        if trade.Success {
            wins++
            totalWin += trade.PnL
        } else {
            totalLoss += math.Abs(trade.PnL)
        }
    }
    
    if wins == 0 || totalLoss == 0 {
        return 0.5
    }
    
    winRate := float64(wins) / float64(len(m.tradeHistory))
    avgWin := totalWin / float64(wins)
    avgLoss := totalLoss / float64(len(m.tradeHistory)-wins)
    
    if avgLoss == 0 {
        return 0.5
    }
    
    // Kelly = W - [(1-W) / R]
    // W = win rate, R = avg win / avg loss
    winLossRatio := avgWin / avgLoss
    kelly := winRate - ((1 - winRate) / winLossRatio)
    
    // Use fractional Kelly (25% of full Kelly) for safety
    kelly = kelly * 0.25
    
    // Cap between 0.1 and 0.5
    if kelly < 0.1 {
        kelly = 0.1
    }
    if kelly > 0.5 {
        kelly = 0.5
    }
    
    return kelly
}

// NEW: Risk/Reward analyzer
func (m *Manager) AnalyzeRiskReward(entryPrice, stopLoss, takeProfit float64) (ratio float64, acceptable bool) {
    risk := math.Abs(entryPrice - stopLoss)
    reward := math.Abs(takeProfit - entryPrice)
    
    if risk == 0 {
        return 0, false
    }
    
    ratio = reward / risk
    
    // Minimum acceptable risk/reward is 1.5:1
    acceptable = ratio >= 1.5
    
    return ratio, acceptable
}

func (m *Manager) GetInitialBalance() float64 {
    return m.initialBalance
}