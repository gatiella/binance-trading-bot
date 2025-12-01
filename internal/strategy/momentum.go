// File: internal/strategy/momentum.go
// ============================================
package strategy

import (
    "binance-trading-bot/pkg/types"
    "binance-trading-bot/internal/binance"
    "fmt"
    "log"
    "sort"
)

type MomentumStrategy struct {
    config       *types.Config
    client       *binance.Client
    priceHistory map[string][]float64
    volumeHistory map[string][]float64
}

func NewMomentumStrategy(config *types.Config, client *binance.Client) *MomentumStrategy {
    return &MomentumStrategy{
        config:       config,
        client:       client,
        priceHistory: make(map[string][]float64),
        volumeHistory: make(map[string][]float64),
    }
}

func (s *MomentumStrategy) FindHotCoins(tickers []types.Ticker) []types.Ticker {
    var hotCoins []types.Ticker
    
    for _, ticker := range tickers {
        // Only USDT pairs
        if len(ticker.Symbol) < 4 || ticker.Symbol[len(ticker.Symbol)-4:] != "USDT" {
            continue
        }
        
        // Volume filter
        if ticker.QuoteVolume < s.config.Strategy.MinVolume {
            continue
        }
        
        // Price change filter
        if ticker.PriceChangePercent < s.config.Strategy.MinPriceChange {
            continue
        }
        
        hotCoins = append(hotCoins, ticker)
    }
    
    // Sort by a composite score (price change + volume)
    sort.Slice(hotCoins, func(i, j int) bool {
        scoreI := hotCoins[i].PriceChangePercent + (hotCoins[i].QuoteVolume / 1000000)
        scoreJ := hotCoins[j].PriceChangePercent + (hotCoins[j].QuoteVolume / 1000000)
        return scoreI > scoreJ
    })
    
    // Keep top 10
    if len(hotCoins) > 10 {
        hotCoins = hotCoins[:10]
    }
    
    return hotCoins
}

func (s *MomentumStrategy) AnalyzeMultipleTimeframes(symbol string) ([]types.TimeframeAnalysis, float64) {
    timeframes := []string{"5m", "15m", "1h", "4h"}
    analyses := make([]types.TimeframeAnalysis, 0)
    
    totalScore := 0.0
    validTimeframes := 0
    weights := map[string]float64{"5m": 1.0, "15m": 1.5, "1h": 2.0, "4h": 2.5}
    
    for _, tf := range timeframes {
        klines, err := s.client.GetKlines(symbol, tf, 100)
        if err != nil {
            log.Printf("   ⚠️  Failed to get %s klines: %v", tf, err)
            continue
        }
        
        if len(klines) < 20 {
            continue
        }
        
        closes := make([]float64, len(klines))
        volumes := make([]float64, len(klines))
        
        for i, k := range klines {
            closes[i] = k.Close
            volumes[i] = k.Volume
        }
        
        trend, strength := DetectTrend(klines)
        rsi := CalculateRSI(closes, 14)
        macd, signal, histogram := CalculateMACD(closes)
        upperBB, middleBB, lowerBB := CalculateBollingerBands(closes, 20, 2.0)
        atr := CalculateATR(klines, 14)
        momentumScore := CalculateMomentumScore(closes, volumes)
        
        analysis := types.TimeframeAnalysis{
            Timeframe: tf,
            Trend:     trend,
            Strength:  strength,
            RSI:       rsi,
            MACD:      macd,
            Signal:    signal,
        }
        
        analyses = append(analyses, analysis)
        validTimeframes++
        
        // Weighted score calculation
        weight := weights[tf]
        if trend == "BULLISH" {
            totalScore += strength * weight
        } else if trend == "BEARISH" {
            totalScore -= strength * weight
        }
        
        currentPrice := closes[len(closes)-1]
        bbPosition := "MID"
        if currentPrice > upperBB {
            bbPosition = "ABOVE"
        } else if currentPrice < lowerBB {
            bbPosition = "BELOW"
        }
        
        log.Printf("   📊 %s: %s (%.2f) | RSI: %.1f | MACD: %.4f | Histogram: %.4f", 
            tf, trend, strength, rsi, macd, histogram)
        log.Printf("      BB: %.4f/%.4f/%.4f (%s) | ATR: %.4f | Momentum: %.0f", 
            upperBB, middleBB, lowerBB, bbPosition, atr, momentumScore)
    }
    
    // Normalize score to 0-1 with weights
    totalWeight := 0.0
    for _, tf := range timeframes[:validTimeframes] {
        totalWeight += weights[tf]
    }
    
    var mtfScore float64
    if validTimeframes > 0 {
        mtfScore = (totalScore + totalWeight) / (2 * totalWeight)
    } else {
        mtfScore = 0.5
    }
    
    return analyses, mtfScore
}

func (s *MomentumStrategy) UpdateHistory(symbol string, price, volume float64) {
    if s.priceHistory[symbol] == nil {
        s.priceHistory[symbol] = make([]float64, 0)
        s.volumeHistory[symbol] = make([]float64, 0)
    }
    
    s.priceHistory[symbol] = append(s.priceHistory[symbol], price)
    s.volumeHistory[symbol] = append(s.volumeHistory[symbol], volume)
    
    if len(s.priceHistory[symbol]) > 100 {
        s.priceHistory[symbol] = s.priceHistory[symbol][1:]
        s.volumeHistory[symbol] = s.volumeHistory[symbol][1:]
    }
}

func (s *MomentumStrategy) GenerateSignal(ticker types.Ticker, positions []types.Position) types.Signal {
    signal := types.Signal{
        Symbol:    ticker.Symbol,
        Action:    "HOLD",
        Price:     ticker.LastPrice,
        Timestamp: ticker.Timestamp,
        Strength:  0,
        MTFScore:  0.5,
    }
    
    // Update history
    s.UpdateHistory(ticker.Symbol, ticker.LastPrice, ticker.Volume)
    
    prices := s.priceHistory[ticker.Symbol]
    volumes := s.volumeHistory[ticker.Symbol]
    
    // Fetch historical data if needed
    if len(prices) < 20 {
        log.Printf("   📥 Fetching recent price history for %s...", ticker.Symbol)
        klines, err := s.client.GetKlines(ticker.Symbol, "1m", 50)
        if err == nil && len(klines) > 0 {
            s.priceHistory[ticker.Symbol] = make([]float64, 0)
            s.volumeHistory[ticker.Symbol] = make([]float64, 0)
            
            for _, k := range klines {
                s.priceHistory[ticker.Symbol] = append(s.priceHistory[ticker.Symbol], k.Close)
                s.volumeHistory[ticker.Symbol] = append(s.volumeHistory[ticker.Symbol], k.Volume)
            }
            
            s.priceHistory[ticker.Symbol] = append(s.priceHistory[ticker.Symbol], ticker.LastPrice)
            s.volumeHistory[ticker.Symbol] = append(s.volumeHistory[ticker.Symbol], ticker.Volume)
            
            prices = s.priceHistory[ticker.Symbol]
            volumes = s.volumeHistory[ticker.Symbol]
            log.Printf("   ✅ Loaded %d historical prices", len(prices))
        } else {
            signal.Reason = "Failed to fetch price history"
            log.Printf("   ❌ Could not fetch klines: %v", err)
            return signal
        }
    }
    
    // Check if we already have a position
    hasPosition := false
    for _, pos := range positions {
        if pos.Symbol == ticker.Symbol {
            hasPosition = true
            break
        }
    }
    
    // Calculate indicators on 1-minute data
    var rsi float64
    if len(prices) >= 15 {
        rsi = CalculateRSI(prices, 14)
        // Ensure RSI is valid
        if rsi < 0 || rsi > 100 {
            rsi = 50.0
        }
    } else {
        rsi = 50.0
    }
    
    sma20 := CalculateSMA(prices, 20)
    ema12 := CalculateEMA(prices, 12)
    ema26 := CalculateEMA(prices, 26)
    macd, macdSignal, macdHistogram := CalculateMACD(prices)
    upperBB, middleBB, lowerBB := CalculateBollingerBands(prices, 20, 2.0)
    
    // Volume analysis on 1-minute data
    var currentVolume float64
    var volumeSpike bool
    var volumeRatio float64
    
    if len(volumes) > 1 {
        currentVolume = volumes[len(volumes)-1]
        volumeSpike, volumeRatio = DetectVolumeSpike(volumes[:len(volumes)-1], currentVolume)
    } else {
        currentVolume = ticker.Volume
        volumeSpike = false
        volumeRatio = 1.0
    }
    
    // Multi-timeframe analysis
    var mtfScore float64
    var mtfAnalyses []types.TimeframeAnalysis
    
    if s.config.Strategy.UseMultiTimeframe {
        log.Printf("   🔬 Multi-timeframe analysis:")
        mtfAnalyses, mtfScore = s.AnalyzeMultipleTimeframes(ticker.Symbol)
    } else {
        mtfScore = 0.6
    }
    
    signal.MTFScore = mtfScore
    
    // Only generate BUY signals if we don't have a position
    if !hasPosition {
        // === ENTRY CRITERIA (Multiple Confirmations) ===
        
        // 1. Momentum
        momentumStrong := ticker.PriceChangePercent >= s.config.Strategy.MinPriceChange
        
        // 2. Volume
        volumeGood := ticker.QuoteVolume >= s.config.Strategy.MinVolume
        volumeConfirmation := volumeSpike && volumeRatio > 1.5
        
        // 3. RSI - Not overbought, ideally in sweet spot
        rsiHealthy := rsi >= 40 && rsi <= 75
        rsiOptimal := rsi >= 45 && rsi <= 65
        rsiNotExtreme := rsi > 5 && rsi < 95
        
        // 4. Price action
        aboveSMA := ticker.LastPrice > sma20*0.98
        bullishEMA := ema12 > ema26
        
        // 5. MACD
        macdBullish := macd > macdSignal
        macdPositive := macdHistogram > 0
        
        // 6. Bollinger Bands
        bbPosition := ticker.LastPrice > lowerBB && ticker.LastPrice < upperBB
        bbBullish := ticker.LastPrice > middleBB
        
        // 7. Multi-timeframe
        mtfBullish := mtfScore > 0.50
        mtfStrong := mtfScore > 0.65
        
        // Log all criteria
        log.Printf("   ✅ Momentum: %v (+%.2f%% >= %.1f%%)", 
            momentumStrong, ticker.PriceChangePercent, s.config.Strategy.MinPriceChange)
        log.Printf("   ✅ Volume: %v ($%.0f >= $%.0f) | Spike: %v (%.1fx)", 
            volumeGood, ticker.QuoteVolume, s.config.Strategy.MinVolume, volumeSpike, volumeRatio)
        log.Printf("   ✅ RSI: Healthy=%v Optimal=%v NotExtreme=%v (%.1f)", 
            rsiHealthy, rsiOptimal, rsiNotExtreme, rsi)
        log.Printf("   ✅ Price: AboveSMA=%v ($%.4f vs $%.4f) | BullishEMA=%v", 
            aboveSMA, ticker.LastPrice, sma20, bullishEMA)
        log.Printf("   ✅ MACD: Bullish=%v Positive=%v (%.4f)", macdBullish, macdPositive, macdHistogram)
        log.Printf("   ✅ BB: InRange=%v AboveMid=%v ($%.4f/$%.4f/$%.4f)", 
            bbPosition, bbBullish, lowerBB, middleBB, upperBB)
        log.Printf("   ✅ MTF: Bullish=%v Strong=%v (%.2f)", mtfBullish, mtfStrong, mtfScore)
        
        // === SCORING SYSTEM ===
        score := 0.0
        maxScore := 0.0
        reasons := []string{}
        
        // Must-have criteria (70% of score)
        if momentumStrong {
            score += 15
            reasons = append(reasons, fmt.Sprintf("+%.1f%% momentum", ticker.PriceChangePercent))
        }
        maxScore += 15
        
        if volumeGood {
            score += 15
            if volumeConfirmation {
                score += 5
                reasons = append(reasons, fmt.Sprintf("%.1fx volume spike", volumeRatio))
            }
        }
        maxScore += 20
        
        if rsiHealthy && rsiNotExtreme {
            score += 10
            if rsiOptimal {
                score += 5
                reasons = append(reasons, fmt.Sprintf("optimal RSI (%.1f)", rsi))
            } else {
                reasons = append(reasons, fmt.Sprintf("healthy RSI (%.1f)", rsi))
            }
        }
        maxScore += 15
        
        if mtfBullish {
            score += 10
            if mtfStrong {
                score += 10
            }
        }
        maxScore += 20
        
        // Nice-to-have criteria (30% of score)
        if aboveSMA {
            score += 5
            reasons = append(reasons, "above SMA20")
        }
        maxScore += 5
        
        if bullishEMA {
            score += 5
            reasons = append(reasons, "bullish EMA crossover")
        }
        maxScore += 5
        
        if macdBullish && macdPositive {
            score += 10
            reasons = append(reasons, "MACD bullish")
        }
        maxScore += 10
        
        if bbPosition && bbBullish {
            score += 5
        }
        maxScore += 5
        
        // Calculate final strength
        signal.Strength = score / maxScore
        
        log.Printf("   📊 SCORE: %.0f/%.0f (%.1f%%)", score, maxScore, signal.Strength*100)
        
        // Generate BUY signal if score is good AND RSI is not extreme
        threshold := 0.60
        
        // CRITICAL: Reject if RSI is at extremes (likely calculation error or extreme overbought/oversold)
        if !rsiNotExtreme {
            signal.Reason = fmt.Sprintf("Extreme RSI detected (%.1f) - rejecting signal for safety", rsi)
            log.Printf("   🚫 REJECTED: %s", signal.Reason)
            return signal
        }
        
        if signal.Strength >= threshold {
            signal.Action = "BUY"
            
            // Build detailed reason
            reason := fmt.Sprintf("Score: %.0f%% | ", signal.Strength*100)
            for i, r := range reasons {
                if i > 0 {
                    reason += ", "
                }
                reason += r
            }
            
            reason += fmt.Sprintf("\n   RSI: %.1f | BB: $%.4f-$%.4f", rsi, lowerBB, upperBB)
            reason += fmt.Sprintf("\n   MTF: %.0f%% (", mtfScore*100)
            
            if len(mtfAnalyses) > 0 {
                for i, a := range mtfAnalyses {
                    if i > 0 {
                        reason += ", "
                    }
                    reason += fmt.Sprintf("%s:%s", a.Timeframe, a.Trend)
                }
                reason += ")"
            } else {
                reason += "No MTF)"
            }
            
            signal.Reason = reason
            log.Printf("   🎯 BUY SIGNAL GENERATED - Strength: %.0f%%", signal.Strength*100)
            
        } else {
            // Explain why score is too low
            missing := []string{}
            if !momentumStrong {
                missing = append(missing, fmt.Sprintf("weak momentum (%.2f%%)", ticker.PriceChangePercent))
            }
            if !volumeGood {
                missing = append(missing, fmt.Sprintf("low volume ($%.0f)", ticker.QuoteVolume))
            }
            if !rsiHealthy {
                missing = append(missing, fmt.Sprintf("poor RSI (%.1f)", rsi))
            } else if !rsiNotExtreme {
                missing = append(missing, fmt.Sprintf("extreme RSI (%.1f)", rsi))
            }
            if !mtfBullish {
                missing = append(missing, fmt.Sprintf("MTF bearish (%.2f)", mtfScore))
            }
            if !aboveSMA {
                missing = append(missing, "below SMA20")
            }
            
            signal.Reason = fmt.Sprintf("Score too low (%.0f%% < 60%%): ", signal.Strength*100)
            for i, m := range missing {
                if i > 0 {
                    signal.Reason += ", "
                }
                signal.Reason += m
            }
            
            log.Printf("   ⛔ No signal: %s", signal.Reason)
        }
    } else {
        signal.Reason = "Already have position"
        log.Printf("   ⏭️  Skipping: Already have position in %s", ticker.Symbol)
    }
    
    return signal
}