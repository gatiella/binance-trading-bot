// File: internal/strategy/indicators.go
// ============================================
package strategy

import (
    "math"
    "binance-trading-bot/pkg/types"
)

// CalculateRSI - Relative Strength Index (FIXED VERSION)
func CalculateRSI(prices []float64, period int) float64 {
    if len(prices) < period+1 {
        return 50.0
    }
    
    // Calculate price changes
    gains := make([]float64, 0)
    losses := make([]float64, 0)
    
    for i := 1; i < len(prices); i++ {
        change := prices[i] - prices[i-1]
        if change > 0 {
            gains = append(gains, change)
            losses = append(losses, 0)
        } else {
            gains = append(gains, 0)
            losses = append(losses, math.Abs(change))
        }
    }
    
    if len(gains) < period {
        return 50.0
    }
    
    // First RS calculation uses SMA
    avgGain := 0.0
    avgLoss := 0.0
    for i := 0; i < period; i++ {
        avgGain += gains[i]
        avgLoss += losses[i]
    }
    avgGain /= float64(period)
    avgLoss /= float64(period)
    
    // Subsequent calculations use smoothed averages (Wilder's smoothing)
    for i := period; i < len(gains); i++ {
        avgGain = (avgGain*float64(period-1) + gains[i]) / float64(period)
        avgLoss = (avgLoss*float64(period-1) + losses[i]) / float64(period)
    }
    
    // Handle edge cases
    if avgLoss == 0 {
        if avgGain == 0 {
            return 50.0  // No movement
        }
        return 100.0  // All gains, no losses
    }
    
    rs := avgGain / avgLoss
    rsi := 100.0 - (100.0 / (1.0 + rs))
    
    // Safety bounds
    if rsi < 0 {
        rsi = 0
    }
    if rsi > 100 {
        rsi = 100
    }
    
    return rsi
}

// CalculateSMA - Simple Moving Average
func CalculateSMA(prices []float64, period int) float64 {
    if len(prices) < period {
        return 0
    }
    
    sum := 0.0
    for i := len(prices) - period; i < len(prices); i++ {
        sum += prices[i]
    }
    return sum / float64(period)
}

// CalculateEMA - Exponential Moving Average
func CalculateEMA(prices []float64, period int) float64 {
    if len(prices) < period {
        return 0
    }
    
    multiplier := 2.0 / float64(period+1)
    ema := CalculateSMA(prices[:period], period)
    
    for i := period; i < len(prices); i++ {
        ema = (prices[i]-ema)*multiplier + ema
    }
    return ema
}

// CalculateMACD - Moving Average Convergence Divergence
func CalculateMACD(prices []float64) (macd, signal, histogram float64) {
    if len(prices) < 26 {
        return 0, 0, 0
    }
    
    ema12 := CalculateEMA(prices, 12)
    ema26 := CalculateEMA(prices, 26)
    macd = ema12 - ema26
    
    macdValues := make([]float64, 0)
    for i := 26; i < len(prices); i++ {
        tempPrices := prices[:i+1]
        tempEMA12 := CalculateEMA(tempPrices, 12)
        tempEMA26 := CalculateEMA(tempPrices, 26)
        macdValues = append(macdValues, tempEMA12-tempEMA26)
    }
    
    if len(macdValues) >= 9 {
        signal = CalculateEMA(macdValues, 9)
        histogram = macd - signal
    }
    return macd, signal, histogram
}

// CalculateBollingerBands - Returns upper, middle, lower bands
func CalculateBollingerBands(prices []float64, period int, stdDev float64) (upper, middle, lower float64) {
    if len(prices) < period {
        return 0, 0, 0
    }
    
    middle = CalculateSMA(prices, period)
    
    // Calculate standard deviation
    variance := 0.0
    for i := len(prices) - period; i < len(prices); i++ {
        variance += math.Pow(prices[i]-middle, 2)
    }
    variance = variance / float64(period)
    stdDeviation := math.Sqrt(variance)
    
    upper = middle + (stdDev * stdDeviation)
    lower = middle - (stdDev * stdDeviation)
    
    return upper, middle, lower
}

// CalculateATR - Average True Range (volatility indicator)
func CalculateATR(klines []types.Kline, period int) float64 {
    if len(klines) < period+1 {
        return 0
    }
    
    trueRanges := make([]float64, 0)
    
    for i := 1; i < len(klines); i++ {
        highLow := klines[i].High - klines[i].Low
        highClose := math.Abs(klines[i].High - klines[i-1].Close)
        lowClose := math.Abs(klines[i].Low - klines[i-1].Close)
        
        tr := math.Max(highLow, math.Max(highClose, lowClose))
        trueRanges = append(trueRanges, tr)
    }
    
    return CalculateSMA(trueRanges, period)
}

// CalculateStochastic - Stochastic Oscillator
func CalculateStochastic(klines []types.Kline, period int) (k, d float64) {
    if len(klines) < period {
        return 50, 50
    }
    
    recentKlines := klines[len(klines)-period:]
    
    high := recentKlines[0].High
    low := recentKlines[0].Low
    
    for _, kline := range recentKlines {
        if kline.High > high {
            high = kline.High
        }
        if kline.Low < low {
            low = kline.Low
        }
    }
    
    currentClose := klines[len(klines)-1].Close
    
    if high-low == 0 {
        return 50, 50
    }
    
    k = ((currentClose - low) / (high - low)) * 100
    d = k // Simplified - in production, calculate 3-period SMA of K
    
    return k, d
}

// DetectVolumeSpike - Returns true if volume is significantly above average
func DetectVolumeSpike(volumes []float64, currentVolume float64) (bool, float64) {
    if len(volumes) < 10 {
        return false, 1.0
    }
    
    // Only use non-zero volumes for average
    validVolumes := make([]float64, 0)
    for _, v := range volumes {
        if v > 0.000001 {  // Filter out near-zero values
            validVolumes = append(validVolumes, v)
        }
    }
    
    if len(validVolumes) < 5 {
        return false, 1.0
    }
    
    avgVolume := CalculateSMA(validVolumes, len(validVolumes))
    if avgVolume < 0.000001 {
        return false, 1.0
    }
    
    ratio := currentVolume / avgVolume
    
    // Cap ratio at 10x for sanity
    if ratio > 10.0 {
        ratio = 10.0
    }
    
    return ratio > 2.0, ratio
}

// CalculateVWAP - Volume Weighted Average Price
func CalculateVWAP(klines []types.Kline) float64 {
    if len(klines) == 0 {
        return 0
    }
    
    totalVolume := 0.0
    vwap := 0.0
    
    for _, k := range klines {
        typicalPrice := (k.High + k.Low + k.Close) / 3
        totalVolume += k.Volume
        vwap += typicalPrice * k.Volume
    }
    
    if totalVolume == 0 {
        return 0
    }
    
    return vwap / totalVolume
}

// DetectTrend - Enhanced trend detection with strength
func DetectTrend(klines []types.Kline) (string, float64) {
    if len(klines) < 20 {
        return "NEUTRAL", 0.5
    }
    
    closes := make([]float64, len(klines))
    volumes := make([]float64, len(klines))
    
    for i, k := range klines {
        closes[i] = k.Close
        volumes[i] = k.Volume
    }
    
    sma20 := CalculateSMA(closes, 20)
    sma50 := CalculateSMA(closes, 50)
    currentPrice := closes[len(closes)-1]
    rsi := CalculateRSI(closes, 14)
    macd, signal, _ := CalculateMACD(closes)
    
    // Bollinger Bands
    upperBB, _, lowerBB := CalculateBollingerBands(closes, 20, 2.0)
    
    // Volume analysis
    currentVolume := volumes[len(volumes)-1]
    volumeSpike, volumeRatio := DetectVolumeSpike(volumes[:len(volumes)-1], currentVolume)
    
    bullishSignals := 0
    bearishSignals := 0
    totalSignals := 0
    
    // Price vs SMAs (weight: 2)
    if currentPrice > sma20 {
        bullishSignals += 2
    } else {
        bearishSignals += 2
    }
    totalSignals += 2
    
    if len(closes) >= 50 {
        if sma20 > sma50 {
            bullishSignals += 2
        } else {
            bearishSignals += 2
        }
        totalSignals += 2
    }
    
    // RSI (weight: 1)
    if rsi > 50 && rsi < 70 {
        bullishSignals++
    } else if rsi < 50 && rsi > 30 {
        bearishSignals++
    }
    totalSignals++
    
    // MACD (weight: 2)
    if macd > signal {
        bullishSignals += 2
    } else {
        bearishSignals += 2
    }
    totalSignals += 2
    
    // Bollinger Bands (weight: 1)
    if currentPrice > (upperBB+lowerBB)/2 {
        bullishSignals++
    } else {
        bearishSignals++
    }
    totalSignals++
    
    // Volume confirmation (weight: 1)
    if volumeSpike && volumeRatio > 2.0 {
        if currentPrice > closes[len(closes)-2] {
            bullishSignals++
        } else {
            bearishSignals++
        }
        totalSignals++
    }
    
    strength := float64(bullishSignals) / float64(totalSignals)
    
    if strength > 0.65 {
        return "BULLISH", strength
    } else if strength < 0.35 {
        return "BEARISH", 1 - strength
    }
    
    return "NEUTRAL", 0.5
}

// CalculateSupportResistance - Find support and resistance levels
func CalculateSupportResistance(klines []types.Kline) (support, resistance float64) {
    if len(klines) < 20 {
        return 0, 0
    }
    
    support = klines[0].Low
    resistance = klines[0].High
    
    for _, k := range klines {
        if k.Low < support {
            support = k.Low
        }
        if k.High > resistance {
            resistance = k.High
        }
    }
    
    return support, resistance
}

// CalculateMomentumScore - Composite momentum score (0-100)
func CalculateMomentumScore(prices []float64, volumes []float64) float64 {
    if len(prices) < 20 || len(volumes) < 20 {
        return 50
    }
    
    // Price momentum
    priceChange := ((prices[len(prices)-1] - prices[len(prices)-20]) / prices[len(prices)-20]) * 100
    
    // Volume momentum
    recentVol := CalculateSMA(volumes[len(volumes)-5:], 5)
    oldVol := CalculateSMA(volumes[len(volumes)-20:len(volumes)-5], 15)
    volumeChange := 0.0
    if oldVol > 0 {
        volumeChange = ((recentVol - oldVol) / oldVol) * 100
    }
    
    // Combine (70% price, 30% volume)
    score := (priceChange * 0.7) + (volumeChange * 0.3)
    
    // Normalize to 0-100
    score = 50 + score
    if score > 100 {
        score = 100
    }
    if score < 0 {
        score = 0
    }
    
    return score
}

// NEW: DetectMarketRegime - Identify if market is trending, ranging, or volatile
func DetectMarketRegime(klines []types.Kline) (regime string, confidence float64) {
    if len(klines) < 50 {
        return "UNKNOWN", 0.5
    }
    
    closes := make([]float64, len(klines))
    for i, k := range klines {
        closes[i] = k.Close
    }
    
    // Calculate indicators
    atr := CalculateATR(klines, 14)
    sma := CalculateSMA(closes, 20)
    currentPrice := closes[len(closes)-1]
    
    // Price deviation from SMA
    deviation := math.Abs(currentPrice-sma) / sma * 100
    
    // ATR as % of price (volatility measure)
    volatility := (atr / currentPrice) * 100
    
    // Count closes above/below SMA for trend consistency
    aboveSMA := 0
    for i := len(closes) - 20; i < len(closes); i++ {
        if closes[i] > sma {
            aboveSMA++
        }
    }
    consistency := float64(aboveSMA) / 20.0
    
    // Classify regime
    if volatility > 5.0 {
        return "VOLATILE", 0.8
    } else if consistency > 0.7 || consistency < 0.3 {
        return "TRENDING", math.Abs(consistency-0.5) * 2
    } else if deviation < 2.0 {
        return "RANGING", 0.7
    }
    
    return "TRANSITIONING", 0.5
}

// NEW: AnalyzeVolumeProfile - Detect accumulation vs distribution
func AnalyzeVolumeProfile(klines []types.Kline, periods int) (signal string, strength float64) {
    if len(klines) < periods {
        return "NEUTRAL", 0.5
    }
    
    recent := klines[len(klines)-periods:]
    
    upVolume := 0.0
    downVolume := 0.0
    
    for _, k := range recent {
        if k.Close > k.Open {
            upVolume += k.Volume
        } else {
            downVolume += k.Volume
        }
    }
    
    totalVolume := upVolume + downVolume
    if totalVolume == 0 {
        return "NEUTRAL", 0.5
    }
    
    buyPressure := upVolume / totalVolume
    
    if buyPressure > 0.65 {
        return "ACCUMULATION", buyPressure
    } else if buyPressure < 0.35 {
        return "DISTRIBUTION", 1 - buyPressure
    }
    
    return "NEUTRAL", 0.5
}