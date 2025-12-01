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
    
    b.telegram.NotifyStart()
    
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        b.mainLoop()
        b.checkDailyReport()
        b.cleanupAlertedCoins() // Clean old alerts
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
    
    stopLoss := b.risk.CalculateStopLoss(signal.Price, "BUY")
    takeProfit := b.risk.CalculateTakeProfit(signal.Price, "BUY")
    quantity := b.risk.CalculatePositionSize(signal.Price)
    
    log.Printf("\n💡 SUGGESTED TRADE SETUP:")
    log.Printf("   Symbol: %s", signal.Symbol)
    log.Printf("   Entry: $%.4f", signal.Price)
    log.Printf("   Quantity: %.4f (≈ $%.2f)", quantity, quantity*signal.Price)
    log.Printf("   Stop Loss: $%.4f (%.2f%%)", stopLoss, b.config.Strategy.StopLossPercent)
    log.Printf("   Take Profit: $%.4f (%.2f%%)", takeProfit, b.config.Strategy.TakeProfitPercent)
    log.Printf("   Risk/Reward: 1:%.2f", 
        b.config.Strategy.TakeProfitPercent/b.config.Strategy.StopLossPercent)
    
    if b.config.Strategy.TrailingStopEnabled {
        log.Printf("   Trailing Stop: %.1f%%", b.config.Strategy.TrailingStopPercent)
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
    log.Printf("⏰ Time: %s", time.Now().Format("15:04:05"))
    log.Println(strings.Repeat("=", 60))
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