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
    "net/http"
    "net/url"
    "strconv"
    "time"
    "binance-trading-bot/pkg/types"
)

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
    
    resp, err := c.httpClient.Get(url)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, err
    }
    
    var rawTickers []map[string]interface{}
    if err := json.Unmarshal(body, &rawTickers); err != nil {
        return nil, err
    }
    
    var tickers []types.Ticker
    for _, raw := range rawTickers {
        symbol, _ := raw["symbol"].(string)
        
        priceChange, _ := strconv.ParseFloat(raw["priceChange"].(string), 64)
        priceChangePercent, _ := strconv.ParseFloat(raw["priceChangePercent"].(string), 64)
        lastPrice, _ := strconv.ParseFloat(raw["lastPrice"].(string), 64)
        volume, _ := strconv.ParseFloat(raw["volume"].(string), 64)
        quoteVolume, _ := strconv.ParseFloat(raw["quoteVolume"].(string), 64)
        
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
    
    body, _ := io.ReadAll(resp.Body)
    
    var rawKlines [][]interface{}
    json.Unmarshal(body, &rawKlines)
    
    var klines []types.Kline
    for _, k := range rawKlines {
        openTime := time.UnixMilli(int64(k[0].(float64)))
        open, _ := strconv.ParseFloat(k[1].(string), 64)
        high, _ := strconv.ParseFloat(k[2].(string), 64)
        low, _ := strconv.ParseFloat(k[3].(string), 64)
        close, _ := strconv.ParseFloat(k[4].(string), 64)
        volume, _ := strconv.ParseFloat(k[5].(string), 64)
        closeTime := time.UnixMilli(int64(k[6].(float64)))
        
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
    
    body, _ := io.ReadAll(resp.Body)
    
    var account struct {
        Balances []struct {
            Asset  string `json:"asset"`
            Free   string `json:"free"`
            Locked string `json:"locked"`
        } `json:"balances"`
    }
    
    json.Unmarshal(body, &account)
    
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
    
    price, _ := strconv.ParseFloat(orderResp["price"].(string), 64)
    executedQty, _ := strconv.ParseFloat(orderResp["executedQty"].(string), 64)
    
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
    
    body, _ := io.ReadAll(resp.Body)
    
    var priceResp struct {
        Price string `json:"price"`
    }
    
    json.Unmarshal(body, &priceResp)
    return strconv.ParseFloat(priceResp.Price, 64)
}