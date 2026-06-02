package broker

import (
	"encoding/binary"
	"math"
	"strings"
	"time"
)

// Subscription modes
const (
	ModeLTP       = 1
	ModeQuote     = 2
	ModeSnapQuote = 3
	ModeDepth     = 4
)

// Exchange types
const (
	ExchangeNSE_CM = 1
	ExchangeNSE_FO = 2
	ExchangeBSE_CM = 3
	ExchangeBSE_FO = 4
	ExchangeMCX_FO = 5
)

// Tick represents a parsed market data tick from the WebSocket binary stream.
type Tick struct {
	SubscriptionMode int
	ExchangeType     int
	Token            string
	SequenceNumber   int64
	ExchangeTimestamp time.Time
	LastTradedPrice  float64

	// Quote mode fields (mode >= 2)
	LastTradedQty    int64
	AvgTradedPrice   float64
	VolumeTradedDay  int64
	TotalBuyQty      float64
	TotalSellQty     float64
	OpenPrice        float64
	HighPrice        float64
	LowPrice         float64
	ClosePrice       float64

	// SnapQuote fields (mode >= 3)
	LastTradedTimestamp time.Time
	OpenInterest       int64
	UpperCircuit       float64
	LowerCircuit       float64
	Week52High         float64
	Week52Low          float64
}

// ParseBinaryTick parses a binary WebSocket V2 tick message according to the
// Angel One SmartAPI WebSocket 2.0 specification.
// Binary format: Little Endian byte order.
//
// LTP mode packet: 51 bytes
// Quote mode packet: 123 bytes
// SnapQuote mode packet: 379 bytes
func ParseBinaryTick(data []byte) (*Tick, error) {
	if len(data) < 51 {
		return nil, ErrPacketTooShort
	}

	tick := &Tick{}

	// Byte 0: Subscription Mode (1 byte, uint8)
	tick.SubscriptionMode = int(data[0])

	// Byte 1: Exchange Type (1 byte, uint8)
	tick.ExchangeType = int(data[1])

	// Bytes 2-26: Token (25 bytes, UTF-8, null-terminated)
	tokenBytes := data[2:27]
	tick.Token = strings.TrimRight(string(tokenBytes), "\x00")

	// Bytes 27-34: Sequence Number (int64, 8 bytes)
	tick.SequenceNumber = int64(binary.LittleEndian.Uint64(data[27:35]))

	// Bytes 35-42: Exchange Timestamp (int64, 8 bytes, epoch milliseconds)
	exchTsMs := int64(binary.LittleEndian.Uint64(data[35:43]))
	tick.ExchangeTimestamp = time.UnixMilli(exchTsMs)

	// Bytes 43-50: Last Traded Price (int64, 8 bytes)
	// Prices in paise — divide by 100 for equity, by 10000000 for currencies
	ltpRaw := int64(binary.LittleEndian.Uint64(data[43:51]))
	tick.LastTradedPrice = priceFromPaise(ltpRaw, tick.ExchangeType)

	// LTP mode ends here (51 bytes)
	if tick.SubscriptionMode == ModeLTP || len(data) < 123 {
		return tick, nil
	}

	// ━━━ Quote mode fields ━━━

	// Bytes 51-58: Last Traded Quantity (int64)
	tick.LastTradedQty = int64(binary.LittleEndian.Uint64(data[51:59]))

	// Bytes 59-66: Average Traded Price (int64)
	tick.AvgTradedPrice = priceFromPaise(int64(binary.LittleEndian.Uint64(data[59:67])), tick.ExchangeType)

	// Bytes 67-74: Volume Traded for Day (int64)
	tick.VolumeTradedDay = int64(binary.LittleEndian.Uint64(data[67:75]))

	// Bytes 75-82: Total Buy Quantity (double/float64)
	tick.TotalBuyQty = math.Float64frombits(binary.LittleEndian.Uint64(data[75:83]))

	// Bytes 83-90: Total Sell Quantity (double/float64)
	tick.TotalSellQty = math.Float64frombits(binary.LittleEndian.Uint64(data[83:91]))

	// Bytes 91-98: Open Price (int64)
	tick.OpenPrice = priceFromPaise(int64(binary.LittleEndian.Uint64(data[91:99])), tick.ExchangeType)

	// Bytes 99-106: High Price (int64)
	tick.HighPrice = priceFromPaise(int64(binary.LittleEndian.Uint64(data[99:107])), tick.ExchangeType)

	// Bytes 107-114: Low Price (int64)
	tick.LowPrice = priceFromPaise(int64(binary.LittleEndian.Uint64(data[107:115])), tick.ExchangeType)

	// Bytes 115-122: Close Price (int64)
	tick.ClosePrice = priceFromPaise(int64(binary.LittleEndian.Uint64(data[115:123])), tick.ExchangeType)

	// Quote mode ends here (123 bytes)
	if tick.SubscriptionMode == ModeQuote || len(data) < 379 {
		return tick, nil
	}

	// ━━━ SnapQuote mode fields ━━━

	// Bytes 123-130: Last Traded Timestamp (int64)
	ltTs := int64(binary.LittleEndian.Uint64(data[123:131]))
	tick.LastTradedTimestamp = time.UnixMilli(ltTs)

	// Bytes 131-138: Open Interest (int64)
	tick.OpenInterest = int64(binary.LittleEndian.Uint64(data[131:139]))

	// Bytes 139-146: OI change % (double, DUMMY — skip)
	// Bytes 147-346: Best Five Data (200 bytes — skip for candle engine)

	// Bytes 347-354: Upper Circuit Limit (int64)
	tick.UpperCircuit = priceFromPaise(int64(binary.LittleEndian.Uint64(data[347:355])), tick.ExchangeType)

	// Bytes 355-362: Lower Circuit Limit (int64)
	tick.LowerCircuit = priceFromPaise(int64(binary.LittleEndian.Uint64(data[355:363])), tick.ExchangeType)

	// Bytes 363-370: 52 Week High (int64)
	tick.Week52High = priceFromPaise(int64(binary.LittleEndian.Uint64(data[363:371])), tick.ExchangeType)

	// Bytes 371-378: 52 Week Low (int64)
	tick.Week52Low = priceFromPaise(int64(binary.LittleEndian.Uint64(data[371:379])), tick.ExchangeType)

	return tick, nil
}

// priceFromPaise converts raw paise integer to float64 price.
// For CDS/currency, divide by 10,000,000. For everything else, divide by 100.
func priceFromPaise(raw int64, exchangeType int) float64 {
	if exchangeType == 13 { // CDE_FO
		return float64(raw) / 10000000.0
	}
	return float64(raw) / 100.0
}

// ExchangeName returns a human-readable exchange name.
func ExchangeName(exchangeType int) string {
	switch exchangeType {
	case ExchangeNSE_CM:
		return "NSE"
	case ExchangeNSE_FO:
		return "NSE_FO"
	case ExchangeBSE_CM:
		return "BSE"
	case ExchangeBSE_FO:
		return "BSE_FO"
	case ExchangeMCX_FO:
		return "MCX"
	default:
		return "UNKNOWN"
	}
}

// Custom errors
type BinaryParseError string

func (e BinaryParseError) Error() string { return string(e) }

const ErrPacketTooShort BinaryParseError = "binary packet too short for LTP mode (need >=51 bytes)"
