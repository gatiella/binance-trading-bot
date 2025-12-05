// File: cmd/bot/main.go
// ============================================
package main

import (
    "binance-trading-bot/internal/binance"
    "binance-trading-bot/internal/risk"
    "binance-trading-bot/internal/strategy"
    "binance-trading-bot/internal/telegram"
    "binance-trading-bot/pkg/types"
    "fmt"
    "log"
    "os"
    "strings"
    "time"
    
    "github.com/joho/godotenv"
    "gopkg.in/yaml.v3"
)

type Bot struct {
    client         *binance.Client
    strategy       *strategy.MomentumStrategy
    risk           *risk.Manager
    telegram       *telegram.Notifier
    config         *types.Config
    positions      []types.Position
    lastReportTime time.Time
    alertedCoins   map[string]time.Time // Track when we last alerted for each coin
    startTime      time.Time
}

func NewBot(configPath string) (*Bot, error) {
    if err := godotenv.Load(); err != nil {
        log.Printf("Warning: .env file not found, using config values")
    }
    
    data, err := os.ReadFile(configPath)
    if err != nil {
        return nil, fmt.Errorf("failed to read config: %v", err)
    }
    
    var config types.Config
    if err := yaml.Unmarshal(data, &config); err != nil {
        return nil, fmt.Errorf("failed to parse config: %v", err)
    }
    
    // Override with environment variables
    if apiKey := os.Getenv("BINANCE_API_KEY"); apiKey != "" {
        config.Binance.APIKey = apiKey
    }
    if secretKey := os.Getenv("BINANCE_SECRET_KEY"); secretKey != "" {
        config.Binance.SecretKey = secretKey
    }
    if testnet := os.Getenv("BINANCE_TESTNET"); testnet == "false" {
        config.Binance.Testnet = false
    }
    if botToken := os.Getenv("TELEGRAM_BOT_TOKEN"); botToken != "" {
        config.Telegram.BotToken = botToken
    }
    if chatID := os.Getenv("TELEGRAM_CHAT_ID"); chatID != "" {
        config.Telegram.ChatID = chatID
    }
    
    client := binance.NewClient(
        config.Binance.APIKey,
        config.Binance.SecretKey,
        config.Binance.Testnet,
    )
    
    strat := strategy.NewMomentumStrategy(&config, client)
    
    balances, err := client.GetAccountBalance()
    if err != nil {
        log.Printf("Warning: Could not get balance: %v", err)
    }
    
    initialBalance := balances["USDT"]
    riskMgr := risk.NewManager(&config, initialBalance)
    
    notifier := telegram.NewNotifier(
        config.Telegram.BotToken,
        config.Telegram.ChatID,
        config.Telegram.Enabled,
    )
    
    return &Bot{
        client:         client,
        strategy:       strat,
        risk:           riskMgr,
        telegram:       notifier,
        config:         &config,
        positions:      make([]types.Position, 0),
        lastReportTime: time.Now(),
        alertedCoins:   make(map[string]time.Time),
        startTime:      time.Now(),
    }, nil
}

func (b *Bot) Run() {
    log.Println("🚀 Binance Hot Coins Trading Bot Started!")
    log.Printf("⚙️  Config: Max Positions: %d, Position Size: %.2f USDT", 
        b.config.Strategy.MaxPositions, b.config.Strategy.PositionSize)
    log.Printf("📊 Multi-Timeframe: %v, Trailing Stop: %v", 
        b.config.Strategy.UseMultiTimeframe, b.config.Strategy.TrailingStopEnabled)
    log.Printf("📈 Criteria: Min Volume: $%.0f, Min Price Change: %.1f%%",
        b.config.Strategy.MinVolume, b.config.Strategy.MinPriceChange)
    
    // NEW: Show performance stats if available
    winRate, totalTrades := b.risk.GetWinRate()
    if totalTrades > 0 {
        log.Printf("📊 Historical Performance: %.1f%% win rate over %d trades", 
            winRate*100, totalTrades)
    }
    
    b.telegram.NotifyStart()
    
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    // NEW: Status update ticker (every 5 minutes)
    statusTicker := time.NewTicker(5 * time.Minute)
    defer statusTicker.Stop()
    
    for {
        select {
        case <-ticker.C:
            b.mainLoop()
            b.checkDailyReport()
            b.cleanupAlertedCoins()
            
        case <-statusTicker.C:
            b.displayDetailedStatus()
        }
    }
}

func (b *Bot) mainLoop() {
    tickers, err := b.client.Get24hrTickers()
    if err != nil {
        log.Printf("❌ Error fetching tickers: %v", err)
        b.telegram.NotifyError(fmt.Sprintf("Failed to fetch tickers: %v", err))
        return
    }
    
    log.Printf("📡 Scanned %d total tickers", len(tickers))
    
    // Count USDT pairs
    usdtPairs := 0
    for _, t := range tickers {
        if len(t.Symbol) >= 4 && t.Symbol[len(t.Symbol)-4:] == "USDT" {
            usdtPairs++
        }
    }
    log.Printf("💱 Found %d USDT pairs", usdtPairs)
    
    hotCoins := b.strategy.FindHotCoins(tickers)
    log.Printf("🔥 Hot coins after filtering: %d", len(hotCoins))
    
    if len(hotCoins) > 0 {
        log.Println("\n🔥 HOT COINS DETECTED:")
        hotCoinSummary := make([]string, 0)
        
        for i, coin := range hotCoins {
            if i < 10 {
                log.Printf("  %d. %s: +%.2f%% | Volume: $%.0f | Price: $%.4f",
                    i+1, coin.Symbol, coin.PriceChangePercent, 
                    coin.QuoteVolume, coin.LastPrice)
                if i < 5 {
                    hotCoinSummary = append(hotCoinSummary, 
                        fmt.Sprintf("%s: +%.2f%%", coin.Symbol, coin.PriceChangePercent))
                }
            }
        }
        
        if time.Since(b.lastReportTime) > 5*time.Minute {
            b.telegram.NotifyHotCoins(hotCoinSummary)
        }
    } else {
        log.Println("⚠️  No hot coins found matching criteria:")
        log.Printf("   - Min Volume: $%.0f", b.config.Strategy.MinVolume)
        log.Printf("   - Min Price Change: %.1f%%", b.config.Strategy.MinPriceChange)
    }
    
    b.analyzeAndAlert(hotCoins)
    b.displayStatus(len(hotCoins))
}

func (b *Bot) analyzeAndAlert(hotCoins []types.Ticker) {
    canOpen, reason := b.risk.CanOpenPosition(b.positions)
    
    if !canOpen {
        log.Printf("⚠️  Cannot open new positions: %s", reason)
        return
    }
    
    for _, coin := range hotCoins {
        // Skip if we recently alerted about this coin (within last 10 minutes)
        if lastAlert, exists := b.alertedCoins[coin.Symbol]; exists {
            if time.Since(lastAlert) < 10*time.Minute {
                log.Printf("\n🔕 Skipping %s - Already alerted %.0f seconds ago", 
                    coin.Symbol, time.Since(lastAlert).Seconds())
                continue
            }
        }
        
        log.Printf("\n🔍 Analyzing %s...", coin.Symbol)
        
        signal := b.strategy.GenerateSignal(coin, b.positions)
        
        // Log detailed analysis
        log.Printf("   Signal: %s | Strength: %.2f | MTF Score: %.2f", 
            signal.Action, signal.Strength, signal.MTFScore)
        log.Printf("   Reason: %s", signal.Reason)
        
        // Send alert if BUY signal with good strength
        if signal.Action == "BUY" && signal.Strength > 0.3 {
            b.sendTradeAlert(signal)
            b.alertedCoins[coin.Symbol] = time.Now()
            
            // Only alert for one coin per cycle to avoid spam
            break
        } else if signal.Action == "BUY" {
            log.Printf("   ⚠️  Signal strength too low (%.2f < 0.3)", signal.Strength)
        }
    }
}

func (b *Bot) sendTradeAlert(signal types.Signal) {
    log.Printf("\n🚨 TRADE ALERT - MANUAL ACTION REQUIRED 🚨")
    log.Printf("📊 BUY SIGNAL: %s at $%.4f", signal.Symbol, signal.Price)
    log.Printf("   Strength: %.2f | MTF Score: %.2f", signal.Strength, signal.MTFScore)
    log.Printf("   Reason: %s", signal.Reason)
    
    // NEW: Use dynamic position sizing and stop loss
    volatility := (signal.ATR / signal.Price) * 100  // ATR as percentage
    quantity := b.risk.CalculatePositionSize(signal.Price, signal.Strength, volatility)
    stopLoss := b.risk.CalculateStopLoss(signal.Price, "BUY", signal.ATR)
    takeProfit := b.risk.CalculateTakeProfit(signal.Price, "BUY", signal.Strength)
    
    // Calculate actual position size in USDT
    actualPositionSize := quantity * signal.Price
    
    // Calculate stop loss and take profit percentages
    stopLossPercent := ((signal.Price - stopLoss) / signal.Price) * 100
    takeProfitPercent := ((takeProfit - signal.Price) / signal.Price) * 100
    
    // NEW: Risk/Reward analysis
    rrRatio, acceptable := b.risk.AnalyzeRiskReward(signal.Price, stopLoss, takeProfit)
    
    log.Printf("\n💡 SUGGESTED TRADE SETUP:")
    log.Printf("   Symbol: %s", signal.Symbol)
    log.Printf("   Entry: $%.4f", signal.Price)
    log.Printf("   Quantity: %.4f (≈ $%.2f)", quantity, actualPositionSize)
    log.Printf("   Position Size: %.0f%% of base (Signal: %.0f%%, Volatility: %.1f%%)", 
        (actualPositionSize/b.config.Strategy.PositionSize)*100, 
        signal.Strength*100, 
        volatility)
    log.Printf("   Stop Loss: $%.4f (%.2f%%)", stopLoss, stopLossPercent)
    log.Printf("   Take Profit: $%.4f (%.2f%%)", takeProfit, takeProfitPercent)
    log.Printf("   Risk/Reward: 1:%.2f %s", rrRatio, map[bool]string{true: "✅", false: "⚠️"}[acceptable])
    
    if signal.ATR > 0 {
        log.Printf("   ATR: $%.4f (%.2f%% volatility)", signal.ATR, volatility)
    }
    
    if b.config.Strategy.TrailingStopEnabled {
        log.Printf("   Trailing Stop: %.1f%% (tightens at +5%% and +8%% profit)", 
            b.config.Strategy.TrailingStopPercent)
    }
    
    // NEW: Show Kelly Criterion recommendation
    kelly := b.risk.CalculateKellyCriterion()
    winRate, totalTrades := b.risk.GetWinRate()
    if totalTrades >= 10 {
        log.Printf("\n📊 PERFORMANCE INSIGHTS:")
        log.Printf("   Win Rate: %.1f%% over %d trades", winRate*100, totalTrades)
        log.Printf("   Kelly Criterion: %.1f%% (optimal position sizing)", kelly*100)
        log.Printf("   Current Size: %.1f%% of balance", 
            (actualPositionSize/b.risk.GetInitialBalance())*100)
    }
    
    if !acceptable {
        log.Printf("\n⚠️  WARNING: Risk/Reward ratio below 1.5:1 - Consider skipping")
    }
    
    b.telegram.NotifyTradeAlert(signal, stopLoss, takeProfit, quantity)
    
    log.Printf("\n⚠️  AUTO-TRADING DISABLED - Execute manually on Binance")
    log.Println(strings.Repeat("=", 60))
}

func (b *Bot) displayStatus(hotCoinsCount int) {
    log.Println("\n" + strings.Repeat("=", 60))
    log.Printf("🔍 MONITORING MODE - Watching %d hot coins", hotCoinsCount)
    log.Printf("📊 Open Positions: %d/%d", len(b.positions), b.config.Strategy.MaxPositions)
    log.Printf("💰 Daily PnL: %.2f USDT", b.risk.GetDailyPnL())
    
    // NEW: Show win rate if available
    winRate, totalTrades := b.risk.GetWinRate()
    if totalTrades > 0 {
        log.Printf("📈 Win Rate: %.1f%% (%d trades)", winRate*100, totalTrades)
    }
    
    log.Printf("⏰ Time: %s | Uptime: %s", 
        time.Now().Format("15:04:05"),
        time.Since(b.startTime).Round(time.Minute))
    log.Println(strings.Repeat("=", 60))
}

// NEW: Detailed status report every 5 minutes
func (b *Bot) displayDetailedStatus() {
    log.Println("\n" + strings.Repeat("=", 70))
    log.Println("📊 DETAILED STATUS REPORT")
    log.Println(strings.Repeat("=", 70))
    
    // Bot uptime
    uptime := time.Since(b.startTime)
    log.Printf("⏱️  Uptime: %dd %dh %dm", 
        int(uptime.Hours())/24, 
        int(uptime.Hours())%24, 
        int(uptime.Minutes())%60)
    
    // Performance metrics
    winRate, totalTrades := b.risk.GetWinRate()
    if totalTrades > 0 {
        log.Printf("📈 Performance:")
        log.Printf("   - Total Trades: %d", totalTrades)
        log.Printf("   - Win Rate: %.1f%%", winRate*100)
        log.Printf("   - Daily PnL: %.2f USDT", b.risk.GetDailyPnL())
        
        kelly := b.risk.CalculateKellyCriterion()
        log.Printf("   - Kelly Criterion: %.1f%%", kelly*100)
    } else {
        log.Printf("📈 No trades executed yet")
    }
    
    // Active positions
    if len(b.positions) > 0 {
        log.Printf("\n📊 Active Positions:")
        totalPnL := 0.0
        for i, pos := range b.positions {
            log.Printf("   %d. %s: Entry $%.4f | Current $%.4f | PnL: %.2f%% (%.2f USDT)",
                i+1, pos.Symbol, pos.EntryPrice, pos.CurrentPrice, 
                pos.PnLPercent, pos.PnL)
            totalPnL += pos.PnL
        }
        log.Printf("   Total Unrealized PnL: %.2f USDT", totalPnL)
    } else {
        log.Printf("\n📊 No active positions")
    }
    
    // Recent alerts
    if len(b.alertedCoins) > 0 {
        log.Printf("\n🔔 Recent Alerts:")
        for symbol, alertTime := range b.alertedCoins {
            age := time.Since(alertTime)
            log.Printf("   - %s: %.0f minutes ago", symbol, age.Minutes())
        }
    }
    
    log.Println(strings.Repeat("=", 70))
}

func (b *Bot) cleanupAlertedCoins() {
    // Remove alerts older than 30 minutes
    for symbol, alertTime := range b.alertedCoins {
        if time.Since(alertTime) > 30*time.Minute {
            delete(b.alertedCoins, symbol)
        }
    }
}

func (b *Bot) updatePositions() {
    for i := range b.positions {
        pos := &b.positions[i]
        
        currentPrice, err := b.client.GetCurrentPrice(pos.Symbol)
        if err != nil {
            continue
        }
        
        pos.CurrentPrice = currentPrice
        pos.PnL = (currentPrice - pos.EntryPrice) * pos.Quantity
        pos.PnLPercent = ((currentPrice - pos.EntryPrice) / pos.EntryPrice) * 100
        
        if b.risk.UpdateTrailingStop(pos) {
            log.Printf("🎯 Trailing stop updated for %s: $%.4f", 
                pos.Symbol, pos.TrailingStopPrice)
            b.telegram.NotifyTrailingStopActivated(pos.Symbol, pos.TrailingStopPrice)
        }
        
        shouldClose, reason := b.risk.ShouldClosePosition(*pos)
        if shouldClose {
            b.closePosition(pos, reason)
        }
    }
}

func (b *Bot) closePosition(pos *types.Position, reason string) {
    log.Printf("\n🔔 Closing position: %s", pos.Symbol)
    log.Printf("   Reason: %s", reason)
    
    _, err := b.client.PlaceMarketOrder(pos.Symbol, "SELL", pos.Quantity)
    if err != nil {
        log.Printf("❌ Failed to close position: %v", err)
        b.telegram.NotifyError(fmt.Sprintf("Failed to close %s: %v", pos.Symbol, err))
        return
    }
    
    log.Printf("✅ Position closed: %s", pos.Symbol)
    log.Printf("   PnL: %.2f USDT (%.2f%%)", pos.PnL, pos.PnLPercent)
    
    // NEW: Record trade for performance tracking
    entryTime := time.Now() // You should track actual entry time in Position struct
    duration := time.Since(entryTime).Minutes()
    b.risk.RecordTrade(pos.Symbol, pos.PnL, duration)
    
    b.risk.UpdateDailyPnL(pos.PnL)
    
    b.telegram.NotifyPositionClosed(
        pos.Symbol,
        pos.PnL,
        pos.PnLPercent,
        reason,
    )
    
    newPositions := make([]types.Position, 0)
    for i := range b.positions {
        if b.positions[i].Symbol != pos.Symbol {
            newPositions = append(newPositions, b.positions[i])
        }
    }
    b.positions = newPositions
}

func (b *Bot) checkDailyReport() {
    if time.Since(b.lastReportTime) >= 24*time.Hour {
        totalUnrealizedPnL := 0.0
        for _, pos := range b.positions {
            totalUnrealizedPnL += pos.PnL
        }
        
        // NEW: Include win rate in daily report
        winRate, totalTrades := b.risk.GetWinRate()
        
        log.Printf("\n📊 DAILY REPORT:")
        log.Printf("   Open Positions: %d", len(b.positions))
        log.Printf("   Realized PnL: %.2f USDT", b.risk.GetDailyPnL())
        log.Printf("   Unrealized PnL: %.2f USDT", totalUnrealizedPnL)
        if totalTrades > 0 {
            log.Printf("   Win Rate: %.1f%% (%d trades)", winRate*100, totalTrades)
        }
        
        b.telegram.NotifyDailyReport(
            len(b.positions),
            b.risk.GetDailyPnL(),
            totalUnrealizedPnL,
        )
        
        b.lastReportTime = time.Now()
        b.risk.ResetDailyPnL()
    }
}

func main() {
    bot, err := NewBot("config/config.yaml")
    if err != nil {
        log.Fatalf("Failed to create bot: %v", err)
        return
    }
    
    bot.Run()
}
