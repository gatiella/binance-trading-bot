// File: internal/telegram/notifier.go
// ============================================
package telegram

import (
    "binance-trading-bot/pkg/types" 
    "fmt"
    "io"
    "net/http"
    "net/url"
    "time"
    "log"
    "strings"
)

type Notifier struct {
    botToken string
    chatID   string
    enabled  bool
    client   *http.Client
}

func NewNotifier(botToken, chatID string, enabled bool) *Notifier {
    return &Notifier{
        botToken: botToken,
        chatID:   chatID,
        enabled:  enabled,
        client:   &http.Client{Timeout: 10 * time.Second},
    }
}

func (n *Notifier) NotifyTradeAlert(signal types.Signal, stopLoss, takeProfit, quantity float64) {
    emoji := "🚨"
    
    msg := fmt.Sprintf("%s <b>TRADE OPPORTUNITY</b> %s\n", emoji, emoji)
    msg += strings.Repeat("━", 30) + "\n\n"
    
    msg += fmt.Sprintf("💎 <b>%s</b>\n", signal.Symbol)
    msg += fmt.Sprintf("📊 Signal Strength: <b>%.0f%%</b>\n", signal.Strength*100)
    msg += fmt.Sprintf("📈 Multi-Timeframe: <b>%.0f%%</b>\n\n", signal.MTFScore*100)
    
    msg += "<b>📋 TRADE SETUP:</b>\n"
    msg += fmt.Sprintf("💰 Entry: <code>$%.4f</code>\n", signal.Price)
    msg += fmt.Sprintf("📦 Quantity: <code>%.4f</code> (~$%.2f)\n", quantity, quantity*signal.Price)
    msg += fmt.Sprintf("🛑 Stop Loss: <code>$%.4f</code> (-%.1f%%)\n", 
        stopLoss, ((signal.Price-stopLoss)/signal.Price)*100)
    msg += fmt.Sprintf("🎯 Take Profit: <code>$%.4f</code> (+%.1f%%)\n\n", 
        takeProfit, ((takeProfit-signal.Price)/signal.Price)*100)
    
    // Calculate risk/reward
    riskReward := ((takeProfit - signal.Price) / (signal.Price - stopLoss))
    msg += fmt.Sprintf("⚖️ Risk/Reward: <b>1:%.2f</b>\n\n", riskReward)
    
    msg += "<b>💡 ANALYSIS:</b>\n"
    // Split reason into lines and format nicely
    reasonLines := strings.Split(signal.Reason, "\n")
    for _, line := range reasonLines {
        msg += fmt.Sprintf("<code>%s</code>\n", strings.TrimSpace(line))
    }
    
    msg += "\n" + strings.Repeat("━", 30) + "\n"
    msg += "⚠️ <b>MANUAL EXECUTION REQUIRED</b>\n"
    msg += "Execute this trade on Binance app/web"
    
    n.sendMessage(msg)
}

func (n *Notifier) sendMessage(message string) error {
    if !n.enabled {
        log.Println("⚠️ Telegram notifications disabled in config")
        return nil
    }
    
    log.Printf("📤 Sending Telegram message to chat ID: %s", n.chatID)
    
    apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.botToken)
    
    data := url.Values{}
    data.Set("chat_id", n.chatID)
    data.Set("text", message)
    data.Set("parse_mode", "HTML")
    data.Set("disable_web_page_preview", "true")
    
    resp, err := n.client.PostForm(apiURL, data)
    if err != nil {
        log.Printf("❌ Telegram API error: %v", err)
        return err
    }
    defer resp.Body.Close()
    
    body, _ := io.ReadAll(resp.Body)
    
    if resp.StatusCode != 200 {
        log.Printf("❌ Telegram API response (%d): %s", resp.StatusCode, string(body))
        return fmt.Errorf("telegram API error: %s", string(body))
    }
    
    log.Println("✅ Telegram message sent successfully")
    return nil
}

func (n *Notifier) NotifyStart() {
    msg := "🤖 <b>Trading Bot Started</b>\n\n"
    msg += "✅ Bot is now monitoring Binance for hot coins\n"
    msg += "📊 You'll receive alerts when opportunities are found\n"
    msg += "⚠️ All trades require manual execution"
    n.sendMessage(msg)
}

func (n *Notifier) NotifyHotCoins(coins []string) {
    if len(coins) == 0 {
        return
    }
    
    msg := "🔥 <b>Hot Coins Update</b>\n\n"
    msg += "Currently tracking these gainers:\n\n"
    for i, coin := range coins {
        if i < 5 {
            msg += fmt.Sprintf("• %s\n", coin)
        }
    }
    msg += "\n⏳ Analyzing for entry opportunities..."
    n.sendMessage(msg)
}

func (n *Notifier) NotifyPositionOpened(symbol string, price, stopLoss, takeProfit float64, reason string) {
    msg := fmt.Sprintf("📈 <b>POSITION OPENED</b>\n\n")
    msg += fmt.Sprintf("Symbol: <b>%s</b>\n", symbol)
    msg += fmt.Sprintf("Entry: $%.4f\n", price)
    msg += fmt.Sprintf("Stop Loss: $%.4f\n", stopLoss)
    msg += fmt.Sprintf("Take Profit: $%.4f\n", takeProfit)
    msg += fmt.Sprintf("\n💡 Reason: %s", reason)
    n.sendMessage(msg)
}

func (n *Notifier) NotifyPositionClosed(symbol string, pnl, pnlPercent float64, reason string) {
    emoji := "✅"
    if pnl < 0 {
        emoji = "❌"
    }
    
    msg := fmt.Sprintf("%s <b>POSITION CLOSED</b>\n\n", emoji)
    msg += fmt.Sprintf("Symbol: <b>%s</b>\n", symbol)
    msg += fmt.Sprintf("PnL: <b>%.2f USDT (%.2f%%)</b>\n", pnl, pnlPercent)
    msg += fmt.Sprintf("\n💡 Reason: %s", reason)
    n.sendMessage(msg)
}

func (n *Notifier) NotifyTrailingStopActivated(symbol string, newStopPrice float64) {
    msg := fmt.Sprintf("🎯 <b>Trailing Stop Updated</b>\n\n")
    msg += fmt.Sprintf("Symbol: <b>%s</b>\n", symbol)
    msg += fmt.Sprintf("New Stop: $%.4f", newStopPrice)
    n.sendMessage(msg)
}

func (n *Notifier) NotifyDailyReport(positions int, dailyPnL float64, openPnL float64) {
    emoji := "📊"
    if dailyPnL > 0 {
        emoji = "💰"
    } else if dailyPnL < 0 {
        emoji = "📉"
    }
    
    msg := fmt.Sprintf("%s <b>Daily Report</b>\n\n", emoji)
    msg += fmt.Sprintf("Open Positions: %d\n", positions)
    msg += fmt.Sprintf("Daily PnL: <b>%.2f USDT</b>\n", dailyPnL)
    msg += fmt.Sprintf("Unrealized PnL: %.2f USDT", openPnL)
    n.sendMessage(msg)
}

func (n *Notifier) NotifyError(errorMsg string) {
    msg := fmt.Sprintf("⚠️ <b>Error Alert</b>\n\n%s", errorMsg)
    n.sendMessage(msg)
}