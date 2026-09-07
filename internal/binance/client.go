// File: internal/binance/client.go
// ============================================
package binance

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "net/url"
    "regexp"
    "strconv"
    "time"
    "binance-trading-bot/pkg/types"
)

// validSymbolPattern matches well-formed Binance trading pair symbols
// (uppercase letters/digits only). Testnet occasionally returns junk/test
// entries (e.g. non-ASCII "symbols") that don't match real pairs.
var validSymbolPattern = regexp.MustCompile(`^[A-Z0-9]{5,20}$`)

// safeString safely extracts a string field from a raw JSON map,
// returning ok=false instead of panicking if the field is missing
// or not a string.
func safeString(raw map[string]interface{}, key string) (string, bool) {
    v, exists := raw[key]
    if !exists {
        return "", false
    }
    s, ok := v.(string)
    return s, ok
}

// safeFloat safely extracts and parses a numeric string field,
// returning ok=false if the field is missing, not a string, or not
// parseable as a float.
func safeFloat(raw map[string]interface{}, key string) (float64, bool) {
    s, ok := safeString(raw, key)
    if !ok {
        return 0, false
    }
    f, err := strconv.ParseFloat(s, 64)
    if err != nil {
        return 0, false
    }
    return f, true
}

type Client struct {
    apiKey     string
    secretKey  string
    baseURL    string
    httpClient *http.Client
}

func NewClient(apiKey, secretKey string, testnet bool) *Client {
    baseURL := "https://api.binance.com"
    if testnet {
        baseURL = "https://testnet.binance.vision"
    }
    
    log.Printf("🔧 Binance Client initialized with baseURL: %s", baseURL)
    
    return &Client{
        apiKey:     apiKey,
        secretKey:  secretKey,
        baseURL:    baseURL,
        httpClient: &http.Client{Timeout: 10 * time.Second},
    }
}

func (c *Client) sign(params string) string {
    mac := hmac.New(sha256.New, []byte(c.secretKey))
    mac.Write([]byte(params))
    return hex.EncodeToString(mac.Sum(nil))
}

func (c *Client) Get24hrTickers() ([]types.Ticker, error) {
    url := fmt.Sprintf("%s/api/v3/ticker/24hr", c.baseURL)
    
    log.Printf("📡 Fetching tickers from: %s", url)
    
    resp, err := c.httpClient.Get(url)
    if err != nil {
        log.Printf("❌ HTTP request failed: %v", err)
        return nil, fmt.Errorf("HTTP request failed: %v", err)
    }
    defer resp.Body.Close()
    
    log.Printf("📊 Response status: %d", resp.StatusCode)
    
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        log.Printf("❌ Failed to read response body: %v", err)
        return nil, fmt.Errorf("failed to read response: %v", err)
    }
    
    // Log response preview
    preview := string(body)
    if len(preview) > 500 {
        preview = preview[:500] + "..."
    }
    log.Printf("📄 Response preview (first 500 chars): %s", preview)
    
    // Check for non-200 status
    if resp.StatusCode != 200 {
        log.Printf("❌ API returned error status %d: %s", resp.StatusCode, string(body))
        return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
    }
    
    var rawTickers []map[string]interface{}
    if err := json.Unmarshal(body, &rawTickers); err != nil {
        log.Printf("❌ JSON unmarshal failed: %v", err)
        log.Printf("❌ Full response: %s", string(body))
        return nil, fmt.Errorf("unmarshal error: %v | response: %s", err, preview)
    }
    
    log.Printf("✅ Successfully parsed %d tickers", len(rawTickers))
    
    var tickers []types.Ticker
    skipped := 0
    for _, raw := range rawTickers {
        symbol, ok := safeString(raw, "symbol")
        if !ok || !validSymbolPattern.MatchString(symbol) {
            // Skip malformed/junk entries (e.g. test-only symbols on
            // testnet that aren't real ASCII trading pairs)
            skipped++
            continue
        }
        
        priceChange, ok1 := safeFloat(raw, "priceChange")
        priceChangePercent, ok2 := safeFloat(raw, "priceChangePercent")
        lastPrice, ok3 := safeFloat(raw, "lastPrice")
        volume, ok4 := safeFloat(raw, "volume")
        quoteVolume, ok5 := safeFloat(raw, "quoteVolume")
        
        if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
            log.Printf("⚠️  Skipping %s - malformed ticker fields", symbol)
            skipped++
            continue
        }
        
        // Sanity check: prices/volumes should never be negative, and a
        // real traded pair should have a positive last price.
        if lastPrice <= 0 || volume < 0 || quoteVolume < 0 {
            log.Printf("⚠️  Skipping %s - implausible values (price=%.8f, volume=%.8f, quoteVolume=%.8f)",
                symbol, lastPrice, volume, quoteVolume)
            skipped++
            continue
        }
        
        tickers = append(tickers, types.Ticker{
            Symbol:             symbol,
            PriceChange:        priceChange,
            PriceChangePercent: priceChangePercent,
            LastPrice:          lastPrice,
            Volume:             volume,
            QuoteVolume:        quoteVolume,
            Timestamp:          time.Now(),
        })
    }
    
    if skipped > 0 {
        log.Printf("🧹 Filtered out %d malformed/junk ticker entries", skipped)
    }
    
    return tickers, nil
}

func (c *Client) GetKlines(symbol, interval string, limit int) ([]types.Kline, error) {
    url := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=%d",
        c.baseURL, symbol, interval, limit)
    
    resp, err := c.httpClient.Get(url)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to read klines response: %v", err)
    }
    
    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("klines API error (status %d): %s", resp.StatusCode, string(body))
    }
    
    var rawKlines [][]interface{}
    if err := json.Unmarshal(body, &rawKlines); err != nil {
        return nil, fmt.Errorf("failed to parse klines: %v", err)
    }
    
    var klines []types.Kline
    for _, k := range rawKlines {
        if len(k) < 7 {
            continue
        }
        openTimeMs, ok0 := k[0].(float64)
        openStr, ok1 := k[1].(string)
        highStr, ok2 := k[2].(string)
        lowStr, ok3 := k[3].(string)
        closeStr, ok4 := k[4].(string)
        volumeStr, ok5 := k[5].(string)
        closeTimeMs, ok6 := k[6].(float64)
        if !ok0 || !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
            continue
        }
        
        openTime := time.UnixMilli(int64(openTimeMs))
        open, _ := strconv.ParseFloat(openStr, 64)
        high, _ := strconv.ParseFloat(highStr, 64)
        low, _ := strconv.ParseFloat(lowStr, 64)
        close, _ := strconv.ParseFloat(closeStr, 64)
        volume, _ := strconv.ParseFloat(volumeStr, 64)
        closeTime := time.UnixMilli(int64(closeTimeMs))
        
        klines = append(klines, types.Kline{
            OpenTime:  openTime,
            Open:      open,
            High:      high,
            Low:       low,
            Close:     close,
            Volume:    volume,
            CloseTime: closeTime,
        })
    }
    
    return klines, nil
}

func (c *Client) GetAccountBalance() (map[string]float64, error) {
    timestamp := time.Now().UnixMilli()
    params := fmt.Sprintf("timestamp=%d", timestamp)
    signature := c.sign(params)
    
    url := fmt.Sprintf("%s/api/v3/account?%s&signature=%s", c.baseURL, params, signature)
    
    req, _ := http.NewRequest("GET", url, nil)
    req.Header.Set("X-MBX-APIKEY", c.apiKey)
    
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to read account response: %v", err)
    }
    
    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("account API error (status %d): %s", resp.StatusCode, string(body))
    }
    
    var account struct {
        Balances []struct {
            Asset  string `json:"asset"`
            Free   string `json:"free"`
            Locked string `json:"locked"`
        } `json:"balances"`
    }
    
    if err := json.Unmarshal(body, &account); err != nil {
        return nil, fmt.Errorf("failed to parse account response: %v", err)
    }
    
    balances := make(map[string]float64)
    for _, b := range account.Balances {
        free, _ := strconv.ParseFloat(b.Free, 64)
        locked, _ := strconv.ParseFloat(b.Locked, 64)
        total := free + locked
        if total > 0 {
            balances[b.Asset] = total
        }
    }
    
    return balances, nil
}

func (c *Client) PlaceMarketOrder(symbol, side string, quantity float64) (*types.Trade, error) {
    timestamp := time.Now().UnixMilli()
    
    params := url.Values{}
    params.Set("symbol", symbol)
    params.Set("side", side)
    params.Set("type", "MARKET")
    params.Set("quantity", fmt.Sprintf("%.8f", quantity))
    params.Set("timestamp", fmt.Sprintf("%d", timestamp))
    
    signature := c.sign(params.Encode())
    params.Set("signature", signature)
    
    reqURL := fmt.Sprintf("%s/api/v3/order?%s", c.baseURL, params.Encode())
    
    req, _ := http.NewRequest("POST", reqURL, nil)
    req.Header.Set("X-MBX-APIKEY", c.apiKey)
    
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    body, _ := io.ReadAll(resp.Body)
    
    var orderResp map[string]interface{}
    json.Unmarshal(body, &orderResp)
    
    if resp.StatusCode != 200 {
        return nil, fmt.Errorf("order failed: %s", string(body))
    }
    
    price, _ := safeFloat(orderResp, "price")
    executedQty, _ := safeFloat(orderResp, "executedQty")
    
    return &types.Trade{
        Symbol:    symbol,
        Side:      side,
        Quantity:  executedQty,
        Price:     price,
        Timestamp: time.Now(),
        OrderID:   fmt.Sprintf("%v", orderResp["orderId"]),
    }, nil
}

func (c *Client) GetCurrentPrice(symbol string) (float64, error) {
    url := fmt.Sprintf("%s/api/v3/ticker/price?symbol=%s", c.baseURL, symbol)
    
    resp, err := c.httpClient.Get(url)
    if err != nil {
        return 0, err
    }
    defer resp.Body.Close()
    
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return 0, fmt.Errorf("failed to read price response: %v", err)
    }
    
    if resp.StatusCode != 200 {
        return 0, fmt.Errorf("price API error (status %d): %s", resp.StatusCode, string(body))
    }
    
    var priceResp struct {
        Price string `json:"price"`
    }
    
    json.Unmarshal(body, &priceResp)
    return strconv.ParseFloat(priceResp.Price, 64)
}