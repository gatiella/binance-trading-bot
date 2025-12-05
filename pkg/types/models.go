// File: pkg/types/models.go
// ============================================
package types

import "time"

// Config represents the bot configuration
type Config struct {
    Binance struct {
        APIKey    string `yaml:"api_key"`
        SecretKey string `yaml:"secret_key"`
        Testnet   bool   `yaml:"testnet"`
    } `yaml:"binance"`
    
    Telegram struct {
        BotToken string `yaml:"bot_token"`
        ChatID   string `yaml:"chat_id"`
        Enabled  bool   `yaml:"enabled"`
    } `yaml:"telegram"`
    
    Strategy struct {
        MaxPositions          int     `yaml:"max_positions"`
        PositionSize          float64 `yaml:"position_size_usdt"`
        StopLossPercent       float64 `yaml:"stop_loss_percent"`
        TakeProfitPercent     float64 `yaml:"take_profit_percent"`
        TrailingStopPercent   float64 `yaml:"trailing_stop_percent"`
        TrailingStopEnabled   bool    `yaml:"trailing_stop_enabled"`
        MinVolume             float64 `yaml:"min_volume_usdt"`
        MinPriceChange        float64 `yaml:"min_price_change_percent"`
        UseMultiTimeframe     bool    `yaml:"use_multi_timeframe"`
        MinSignalStrength     float64 `yaml:"min_signal_strength"`
        RequireVolumeSpike    bool    `yaml:"require_volume_spike"`
        VolumeSpikeMultiplier float64 `yaml:"volume_spike_multiplier"`
        MaxRSIEntry           float64 `yaml:"max_rsi_entry"`
        MinRSIEntry           float64 `yaml:"min_rsi_entry"`
        RequireEMACrossover   bool    `yaml:"require_ema_crossover"`
        RequireMACDPositive   bool    `yaml:"require_macd_positive"`
    } `yaml:"strategy"`
    
    Risk struct {
        MaxDailyLoss float64 `yaml:"max_daily_loss_usdt"`
        MaxDrawdown  float64 `yaml:"max_drawdown_percent"`
    } `yaml:"risk"`
}

type Ticker struct {
    Symbol             string
    PriceChange        float64
    PriceChangePercent float64
    LastPrice          float64
    Volume             float64
    QuoteVolume        float64
    Timestamp          time.Time
}

type Position struct {
    Symbol              string
    EntryPrice          float64
    CurrentPrice        float64
    HighestPrice        float64 // For trailing stop
    Quantity            float64
    Side                string
    StopLoss            float64
    TakeProfit          float64
    TrailingStopPrice   float64
    TrailingStopEnabled bool
    PnL                 float64
    PnLPercent          float64
    EntryTime           time.Time
    LastUpdateTime      time.Time // NEW: Track last price update
}

type Signal struct {
    Symbol    string
    Action    string
    Price     float64
    Strength  float64
    Reason    string
    Timestamp time.Time
    MTFScore  float64 // Multi-timeframe score
    ATR       float64 // NEW: Average True Range for volatility
    Regime    string  // NEW: Market regime (TRENDING, RANGING, VOLATILE)
}

type Trade struct {
    Symbol     string
    Side       string
    Quantity   float64
    Price      float64
    Commission float64
    Timestamp  time.Time
    OrderID    string
}

type Kline struct {
    OpenTime  time.Time
    Open      float64
    High      float64
    Low       float64
    Close     float64
    Volume    float64
    CloseTime time.Time
}

type TimeframeAnalysis struct {
    Timeframe string
    Trend     string  // "BULLISH", "BEARISH", "NEUTRAL"
    Strength  float64 // 0-1
    RSI       float64
    MACD      float64
    Signal    float64
}