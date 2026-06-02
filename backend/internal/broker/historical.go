package broker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tradenexus/backend/internal/logger"
	"github.com/tradenexus/backend/internal/models"
)

const (
	historicalEndpoint = "/rest/secure/angelbroking/historical/v1/getCandleData"
	rateLimitDelay     = 350 * time.Millisecond // ~3 requests/sec
)

// Angel One interval constants
const (
	IntervalOneMinute    = "ONE_MINUTE"
	IntervalFiveMinute   = "FIVE_MINUTE"
	IntervalFifteenMin   = "FIFTEEN_MINUTE"
	IntervalThirtyMin    = "THIRTY_MINUTE"
	IntervalOneHour      = "ONE_HOUR"
	IntervalOneDay       = "ONE_DAY"
)

// HistoricalRequest is the request payload for the historical candle API.
type HistoricalRequest struct {
	Exchange    string `json:"exchange"`
	SymbolToken string `json:"symboltoken"`
	Interval    string `json:"interval"`
	FromDate    string `json:"fromdate"`
	ToDate      string `json:"todate"`
}

// HistoricalResponse is the response from the historical candle API.
type HistoricalResponse struct {
	Status    bool       `json:"status"`
	Message   string     `json:"message"`
	ErrorCode string     `json:"errorcode"`
	Data      [][]interface{} `json:"data"`
}

// HistoricalClient fetches historical candle data from Angel One SmartAPI.
type HistoricalClient struct {
	auth       *AuthManager
	httpClient *http.Client
}

// NewHistoricalClient creates a new historical data client.
func NewHistoricalClient(auth *AuthManager) *HistoricalClient {
	return &HistoricalClient{
		auth: auth,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FetchCandles fetches historical candle data for a given symbol and interval.
// Returns finalized candles sorted by timestamp ascending.
func (h *HistoricalClient) FetchCandles(exchange, symbolToken, interval, fromDate, toDate string) ([]models.Candle, error) {
	log := logger.WithComponent("broker.historical")

	headers, err := h.auth.AuthHeaders()
	if err != nil {
		return nil, fmt.Errorf("auth headers failed: %w", err)
	}

	payload := HistoricalRequest{
		Exchange:    exchange,
		SymbolToken: symbolToken,
		Interval:    interval,
		FromDate:    fromDate,
		ToDate:      toDate,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	var resp *http.Response
	var respBody []byte
	maxRetries := 5

	for i := 0; i < maxRetries; i++ {
		// Rate limiting delay for each attempt
		time.Sleep(rateLimitDelay)

		req, err := http.NewRequest("POST", baseURL+historicalEndpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err = h.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("historical API request failed: %w", err)
		}

		respBody, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusOK {
			break // Success, exit loop
		}

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			log.Warn().
				Int("status", resp.StatusCode).
				Int("attempt", i+1).
				Str("symbol", symbolToken).
				Msg("Rate limit exceeded, retrying...")
			
			// Exponential backoff
			backoff := time.Duration(i+1) * 2 * time.Second
			time.Sleep(backoff)
			continue
		}

		// Other HTTP errors, break and return error
		break
	}

	if resp.StatusCode != http.StatusOK {
		// Log a truncated version of the body if it's too long
		bodyStr := string(respBody)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		return nil, fmt.Errorf("historical API request failed with status %d: %s", resp.StatusCode, bodyStr)
	}

	var histResp HistoricalResponse
	if err := json.Unmarshal(respBody, &histResp); err != nil {
		bodyStr := string(respBody)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		return nil, fmt.Errorf("failed to parse historical response: %w (body: %s)", err, bodyStr)
	}

	if !histResp.Status {
		return nil, fmt.Errorf("historical API error: %s (code: %s)", histResp.Message, histResp.ErrorCode)
	}

	// Map interval to timeframe
	tf := intervalToTimeframe(interval)

	candles := make([]models.Candle, 0, len(histResp.Data))
	for _, row := range histResp.Data {
		c, err := parseHistoricalCandle(row, symbolToken, exchange, tf)
		if err != nil {
			log.Warn().Err(err).Msg("Skipping malformed candle row")
			continue
		}
		candles = append(candles, c)
	}

	log.Info().
		Str("symbol", symbolToken).
		Str("interval", interval).
		Int("candles", len(candles)).
		Msg("Historical candles fetched")

	return candles, nil
}

// FetchCandlesBatch fetches candles for multiple date ranges (for large warmup periods).
func (h *HistoricalClient) FetchCandlesBatch(exchange, symbolToken, interval string, fromDate, toDate time.Time, maxDaysPerReq int) ([]models.Candle, error) {
	var allCandles []models.Candle

	current := fromDate
	for current.Before(toDate) {
		end := current.AddDate(0, 0, maxDaysPerReq)
		if end.After(toDate) {
			end = toDate
		}

		fromStr := current.Format("2006-01-02 15:04")
		toStr := end.Format("2006-01-02 15:04")

		candles, err := h.FetchCandles(exchange, symbolToken, interval, fromStr, toStr)
		if err != nil {
			return allCandles, err
		}
		allCandles = append(allCandles, candles...)
		current = end.AddDate(0, 0, 1)
	}

	return allCandles, nil
}

// parseHistoricalCandle converts a raw API response row into a Candle.
// Row format: [timestamp, open, high, low, close, volume]
func parseHistoricalCandle(row []interface{}, token, exchange, timeframe string) (models.Candle, error) {
	if len(row) < 6 {
		return models.Candle{}, fmt.Errorf("row has %d elements, need 6", len(row))
	}

	tsStr, ok := row[0].(string)
	if !ok {
		return models.Candle{}, fmt.Errorf("timestamp is not a string")
	}

	ts, err := time.Parse("2006-01-02T15:04:05-07:00", tsStr)
	if err != nil {
		ts, err = time.Parse("2006-01-02T15:04:05+05:30", tsStr)
		if err != nil {
			return models.Candle{}, fmt.Errorf("failed to parse timestamp: %s", tsStr)
		}
	}

	return models.Candle{
		Symbol:    token,
		Exchange:  exchange,
		Token:     token,
		Timeframe: timeframe,
		Timestamp: ts,
		Open:      toFloat64(row[1]),
		High:      toFloat64(row[2]),
		Low:       toFloat64(row[3]),
		Close:     toFloat64(row[4]),
		Volume:    toInt64(row[5]),
		Finalized: true,
		Source:    "historical",
		CreatedAt: time.Now(),
	}, nil
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}

func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case int:
		return int64(val)
	case int64:
		return val
	default:
		return 0
	}
}

func intervalToTimeframe(interval string) string {
	switch interval {
	case IntervalOneHour:
		return models.Timeframe1H
	case IntervalOneDay:
		return models.Timeframe1D
	default:
		return interval
	}
}
