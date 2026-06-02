package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/gorilla/websocket"
	"github.com/tradenexus/backend/internal/broker"
	"github.com/tradenexus/backend/internal/candle"
	"github.com/tradenexus/backend/internal/config"
	"github.com/tradenexus/backend/internal/indicator"
	"github.com/tradenexus/backend/internal/logger"
	"github.com/tradenexus/backend/internal/models"
	"github.com/tradenexus/backend/internal/scanner"
	"github.com/tradenexus/backend/internal/signal"
	"github.com/tradenexus/backend/internal/store"
	"github.com/tradenexus/backend/internal/telegram"
	"github.com/tradenexus/backend/internal/worker"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Server is the HTTP API server.
type Server struct {
	cfg            *config.Config
	router         chi.Router
	mongoStore     *store.MongoStore
	redisStore     *store.RedisStore
	encryptor      *store.Encryptor
	wsManager      *broker.WebSocketManager
	symbolResolver *broker.SymbolResolver
	candleStore    *candle.Store
	indicatorMgr   *indicator.Manager
	signalPipeline *signal.Pipeline
	weeklyEngine   *scanner.WeeklyEngine
	telegramDisp   *telegram.Dispatcher
	eventBus       *worker.EventBus
	startTime      time.Time

	quotesMu     sync.RWMutex
	latestQuotes map[string]QuoteUpdate
	wsUpgrader   websocket.Upgrader
}

type QuoteUpdate struct {
	Token     string  `json:"token"`
	Price     float64 `json:"price"`
	Timestamp int64   `json:"timestamp"`
}

// NewServer creates a new API server.
func NewServer(
	cfg *config.Config,
	mongoStore *store.MongoStore,
	redisStore *store.RedisStore,
	encryptor *store.Encryptor,
	wsManager *broker.WebSocketManager,
	symbolResolver *broker.SymbolResolver,
	candleStore *candle.Store,
	indicatorMgr *indicator.Manager,
	signalPipeline *signal.Pipeline,
	weeklyEngine *scanner.WeeklyEngine,
	telegramDisp *telegram.Dispatcher,
	eventBus *worker.EventBus,
) *Server {
	s := &Server{
		cfg:            cfg,
		mongoStore:     mongoStore,
		redisStore:     redisStore,
		encryptor:      encryptor,
		wsManager:      wsManager,
		symbolResolver: symbolResolver,
		candleStore:    candleStore,
		indicatorMgr:   indicatorMgr,
		signalPipeline: signalPipeline,
		weeklyEngine:   weeklyEngine,
		telegramDisp:   telegramDisp,
		eventBus:       eventBus,
		startTime:      time.Now(),
		latestQuotes:   make(map[string]QuoteUpdate),
		wsUpgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}

	s.setupRouter()
	s.eventBus.Subscribe(worker.EventTickReceived, s.onTickReceived)
	return s
}

func (s *Server) setupRouter() {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	// Public
	r.With(middleware.Timeout(30 * time.Second)).Post("/api/auth/login", s.handleLogin)
	r.Get("/api/quotes/ws", s.handleQuotesWS)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(30 * time.Second))
		r.Use(s.authMiddleware)

		// Health
		r.Get("/api/health", s.handleHealth)

		// Signals
		r.Get("/api/signals", s.handleGetSignals)
		r.Get("/api/signals/stats", s.handleSignalStats)

		// Watchlists
		r.Get("/api/watchlists", s.handleGetWatchlists)
		r.Post("/api/watchlists", s.handleCreateWatchlist)
		r.Put("/api/watchlists/{id}", s.handleUpdateWatchlist)
		r.Delete("/api/watchlists/{id}", s.handleDeleteWatchlist)
		r.Post("/api/watchlists/{id}/stocks", s.handleAddStock)
		r.Delete("/api/watchlists/{id}/stocks/{symbol}", s.handleRemoveStock)

		// Symbols
		r.Get("/api/symbols/search", s.handleSearchSymbols)

		// Scanners
		r.Get("/api/scanners/results", s.handleGetScannerResults)
		r.Post("/api/scanners/trigger", s.handleTriggerScanner)

		// Settings
		r.Get("/api/settings/telegram", s.handleGetTelegramSettings)
		r.Post("/api/settings/telegram", s.handleSaveTelegramSettings)
		r.Post("/api/settings/telegram/test", s.handleTestTelegram)
	})

	s.router = r
}

// Start starts the HTTP server.
func (s *Server) Start(port string) error {
	log := logger.WithComponent("api")
	log.Info().Str("port", port).Msg("API server starting")
	return http.ListenAndServe(":"+port, s.router)
}

func (s *Server) onTickReceived(event worker.Event) {
	payload, ok := event.Payload.(worker.TickReceivedPayload)
	if !ok || payload.Token == "" || payload.Price <= 0 {
		return
	}

	s.quotesMu.Lock()
	s.latestQuotes[payload.Token] = QuoteUpdate{
		Token:     payload.Token,
		Price:     payload.Price,
		Timestamp: payload.Timestamp,
	}
	s.quotesMu.Unlock()
}

func (s *Server) handleQuotesWS(w http.ResponseWriter, r *http.Request) {
	// WebSocket clients authenticate using query params because browser WS API
	// cannot set custom Authorization headers.
	if r.URL.Query().Get("u") != s.cfg.AuthUsername || r.URL.Query().Get("p") != s.cfg.AuthPassword {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := s.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	tokenSet := make(map[string]bool)
	for _, raw := range strings.Split(r.URL.Query().Get("tokens"), ",") {
		token := strings.TrimSpace(raw)
		if token != "" {
			tokenSet[token] = true
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	lastSent := make(map[string]float64)

	send := func() bool {
		quotes := s.collectQuoteUpdates(tokenSet, lastSent)
		if len(quotes) == 0 {
			return true
		}
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(map[string]interface{}{
			"type":   "quotes",
			"quotes": quotes,
		}); err != nil {
			return false
		}
		return true
	}

	if !send() {
		return
	}

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}

func (s *Server) collectQuoteUpdates(tokenSet map[string]bool, lastSent map[string]float64) []QuoteUpdate {
	s.quotesMu.RLock()
	defer s.quotesMu.RUnlock()

	quotes := make([]QuoteUpdate, 0, len(tokenSet))
	for token, q := range s.latestQuotes {
		if len(tokenSet) > 0 && !tokenSet[token] {
			continue
		}
		if prev, exists := lastSent[token]; exists && prev == q.Price {
			continue
		}
		lastSent[token] = q.Price
		quotes = append(quotes, q)
	}
	return quotes
}

// ━━━ Auth ━━━
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != s.cfg.AuthUsername || pass != s.cfg.AuthPassword {
			w.Header().Set("WWW-Authenticate", `Basic realm="TradeNexus"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if req.Username != s.cfg.AuthUsername || req.Password != s.cfg.AuthPassword {
		jsonError(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}
	jsonResponse(w, map[string]interface{}{
		"success":  true,
		"username": req.Username,
	})
}

// ━━━ Health ━━━
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	mongoOK := s.mongoStore.Ping(r.Context()) == nil
	redisOK := s.redisStore.Ping(r.Context()) == nil

	health := models.SystemHealth{
		WebSocketConnected:  s.wsManager.IsConnected(),
		WebSocketLastTick:   s.wsManager.LastTickTime(),
		MongoConnected:      mongoOK,
		RedisConnected:      redisOK,
		ActiveSubscriptions: s.wsManager.SubscriptionCount(),
		UptimeSeconds:       int64(time.Since(s.startTime).Seconds()),
		MarketOpen:          candle.IsMarketOpen(),
	}

	jsonResponse(w, health)
}

// ━━━ Signals ━━━
func (s *Server) handleGetSignals(w http.ResponseWriter, r *http.Request) {
	tf := r.URL.Query().Get("timeframe")
	category := r.URL.Query().Get("category")
	signals, err := s.signalPipeline.GetRecentSignals(r.Context(), 0, tf, category)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, signals)
}

func (s *Server) handleSignalStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.signalPipeline.GetSignalStats(r.Context())
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, stats)
}

// ━━━ Watchlists ━━━
func (s *Server) handleGetWatchlists(w http.ResponseWriter, r *http.Request) {
	cursor, err := s.mongoStore.Watchlists().Find(r.Context(), bson.M{})
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer cursor.Close(r.Context())

	var watchlists []models.Watchlist
	cursor.All(r.Context(), &watchlists)
	if watchlists == nil {
		watchlists = []models.Watchlist{}
	}
	jsonResponse(w, watchlists)
}

func (s *Server) handleCreateWatchlist(w http.ResponseWriter, r *http.Request) {
	var wl models.Watchlist
	if err := json.NewDecoder(r.Body).Decode(&wl); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Check limit
	count, _ := s.mongoStore.Watchlists().CountDocuments(r.Context(), bson.M{})
	if count >= models.MaxWatchlists {
		jsonError(w, "Maximum 10 watchlists allowed", http.StatusBadRequest)
		return
	}

	wl.IsActive = true
	wl.CreatedAt = time.Now()
	wl.UpdatedAt = time.Now()
	if wl.Stocks == nil {
		wl.Stocks = []models.Stock{}
	}

	result, err := s.mongoStore.Watchlists().InsertOne(r.Context(), wl)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	wl.ID = result.InsertedID.(primitive.ObjectID).Hex()
	jsonResponse(w, wl)
}

func (s *Server) handleUpdateWatchlist(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		jsonError(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var update models.Watchlist
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	update.UpdatedAt = time.Now()
	_, err = s.mongoStore.Watchlists().UpdateOne(r.Context(), bson.M{"_id": objID}, bson.M{"$set": bson.M{
		"name":      update.Name,
		"isActive":  update.IsActive,
		"updatedAt": update.UpdatedAt,
	}})
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "updated"})
}

func (s *Server) handleDeleteWatchlist(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		jsonError(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Get stocks to unsubscribe before deleting
	var wl models.Watchlist
	s.mongoStore.Watchlists().FindOne(r.Context(), bson.M{"_id": objID}).Decode(&wl)

	_, err = s.mongoStore.Watchlists().DeleteOne(r.Context(), bson.M{"_id": objID})
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Unsubscribe stocks
	for _, stock := range wl.Stocks {
		_ = s.wsManager.Unsubscribe(stock.ExchangeType, []string{stock.Token}, broker.ModeQuote)
	}

	jsonResponse(w, map[string]string{"status": "deleted"})
}

func (s *Server) handleAddStock(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		jsonError(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var stock models.Stock
	if err := json.NewDecoder(r.Body).Decode(&stock); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Resolve symbol to token
	inst, err := s.symbolResolver.Resolve(stock.Exchange, stock.Symbol)
	if err != nil {
		jsonError(w, "Symbol not found: "+err.Error(), http.StatusNotFound)
		return
	}
	stock.Token = inst.Token
	stock.Name = inst.Name
	stock.ExchangeType = broker.ExchangeTypeFromSeg(inst.ExchSeg)

	// Add to MongoDB
	_, err = s.mongoStore.Watchlists().UpdateOne(r.Context(), bson.M{"_id": objID}, bson.M{
		"$addToSet": bson.M{"stocks": stock},
		"$set":      bson.M{"updatedAt": time.Now()},
	})
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Subscribe to WebSocket
	_ = s.wsManager.Subscribe(stock.ExchangeType, []string{stock.Token}, broker.ModeQuote)

	jsonResponse(w, stock)
}

func (s *Server) handleRemoveStock(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	symbol := chi.URLParam(r, "symbol")
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		jsonError(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Get the stock first to unsubscribe
	var wl models.Watchlist
	s.mongoStore.Watchlists().FindOne(r.Context(), bson.M{"_id": objID}).Decode(&wl)

	var removedStock *models.Stock
	for _, st := range wl.Stocks {
		if st.Symbol == symbol {
			removedStock = &st
			break
		}
	}

	_, err = s.mongoStore.Watchlists().UpdateOne(r.Context(), bson.M{"_id": objID}, bson.M{
		"$pull": bson.M{"stocks": bson.M{"symbol": symbol}},
		"$set":  bson.M{"updatedAt": time.Now()},
	})
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Unsubscribe if not in any other watchlist
	if removedStock != nil {
		_ = s.wsManager.Unsubscribe(removedStock.ExchangeType, []string{removedStock.Token}, broker.ModeQuote)
	}

	jsonResponse(w, map[string]string{"status": "removed"})
}

// ━━━ Symbols ━━━
func (s *Server) handleSearchSymbols(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if len(query) < 2 {
		jsonResponse(w, []interface{}{})
		return
	}
	results := s.symbolResolver.SearchSymbols(query, 20)
	jsonResponse(w, results)
}

// ━━━ Scanners ━━━
func (s *Server) handleGetScannerResults(w http.ResponseWriter, r *http.Request) {
	filter := bson.M{
		"matched":   true,
		"createdAt": bson.M{"$gte": time.Now().Add(-7 * 24 * time.Hour)},
	}
	cursor, err := s.mongoStore.ManualScannerResults().Find(r.Context(), filter, options.Find().SetSort(bson.M{"createdAt": -1}))
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer cursor.Close(r.Context())

	var matches []models.ScannerMatch
	cursor.All(r.Context(), &matches)
	if matches == nil {
		matches = []models.ScannerMatch{}
	}
	jsonResponse(w, matches)
}

func (s *Server) handleTriggerScanner(w http.ResponseWriter, r *http.Request) {
	go func() {
		log := logger.WithComponent("api.scanner")
		ctx := context.Background()

		if _, err := s.mongoStore.ManualScannerResults().DeleteMany(ctx, bson.M{}); err != nil {
			log.Error().Err(err).Msg("Failed to clear previous manual scanner results")
			return
		}

		cursor, err := s.mongoStore.Watchlists().Find(ctx, bson.M{"isActive": true})
		if err != nil {
			log.Error().Err(err).Msg("Failed to get watchlists for scanner")
			return
		}
		defer cursor.Close(ctx)

		var watchlists []models.Watchlist
		cursor.All(ctx, &watchlists)

		for _, wl := range watchlists {
			for _, stock := range wl.Stocks {
				result, err := s.weeklyEngine.ScanStock(ctx, stock.Token, stock.Exchange)
				if err != nil {
					log.Error().Err(err).Str("symbol", stock.Symbol).Msg("Scanner failed")
					continue
				}
				if result != nil {
					log.Info().Str("symbol", stock.Symbol).Int("score", result.ConvictionScore).Msg("Scanner match found")
					s.persistManualScannerResult(ctx, stock, result)
				}
			}
		}
	}()

	jsonResponse(w, map[string]string{"status": "triggered"})
}

func (s *Server) persistManualScannerResult(ctx context.Context, stock models.Stock, result *models.ConsolidatedScanResult) {
	log := logger.WithComponent("api.scanner")
	current := result.CurrentCandle

	for _, scannerType := range result.MatchedScanners {
		match := models.ScannerMatch{
			Symbol:        stock.Symbol,
			Exchange:      stock.Exchange,
			ScannerType:   scannerType,
			WeekTimestamp: result.WeekTimestamp,
			Matched:       true,
			ScannerMode:   models.ScannerModeManual,
			IsPartialWeek: result.IsPartialWeek,
			ClosePrice:    current.Close,
			Volume:        current.Volume,
			Reason:        scannerReason(scannerType),
			CreatedAt:     time.Now(),
		}

		_, err := s.mongoStore.ManualScannerResults().UpdateOne(ctx,
			bson.M{
				"symbol":        stock.Symbol,
				"scannerType":   scannerType,
				"weekTimestamp": result.WeekTimestamp,
			},
			bson.M{"$set": match},
			options.Update().SetUpsert(true),
		)
		if err != nil {
			log.Warn().Err(err).Str("symbol", stock.Symbol).Str("scanner", scannerType).Msg("Failed to persist scanner match")
		}
	}
}

func scannerReason(scannerType string) string {
	switch scannerType {
	case models.ScannerWeeklyBreakout:
		return "Close broke above prior 52 weekly closes with trend and volume confirmation"
	case models.ScannerWeeklyContinuation:
		return "Weekly continuation with higher low, breakout close, trend, volume, and RSI confirmation"
	case models.ScannerWeekly52WkHigh:
		return "Close broke above prior 52-week high with strong weekly volume"
	case models.ScannerWeeklyPriceAction:
		return "Weekly price-action continuation pattern confirmed"
	default:
		return scannerType
	}
}

func joinLabels(values []string) string {
	if len(values) == 0 {
		return "None"
	}
	out := values[0]
	for _, v := range values[1:] {
		out += ", " + v
	}
	return out
}

func formatInt64(v int64) string {
	if v == 0 {
		return "0"
	}

	negative := v < 0
	if negative {
		v = -v
	}

	var digits []byte
	for v > 0 {
		digits = append(digits, byte('0'+v%10))
		v /= 10
	}
	if negative {
		digits = append(digits, '-')
	}

	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

// ━━━ Settings ━━━
func (s *Server) handleGetTelegramSettings(w http.ResponseWriter, r *http.Request) {
	var settings models.TelegramSettings
	err := s.mongoStore.Settings().FindOne(r.Context(), bson.M{"key": "telegram_config"}).Decode(&settings)
	if err != nil {
		jsonResponse(w, models.TelegramSettings{IsConfigured: false})
		return
	}
	// Don't send encrypted values
	settings.BotTokenEncrypted = ""
	settings.ChatIDEncrypted = ""
	jsonResponse(w, settings)
}

func (s *Server) handleSaveTelegramSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BotToken string `json:"botToken"`
		ChatID   string `json:"chatId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.BotToken == "" || req.ChatID == "" {
		jsonError(w, "Bot token and chat ID are required", http.StatusBadRequest)
		return
	}
	if s.encryptor == nil {
		jsonError(w, "Encryption is not configured", http.StatusInternalServerError)
		return
	}

	encToken, err := s.encryptor.Encrypt(req.BotToken)
	if err != nil {
		jsonError(w, "Encryption failed", http.StatusInternalServerError)
		return
	}
	encChatID, err := s.encryptor.Encrypt(req.ChatID)
	if err != nil {
		jsonError(w, "Encryption failed", http.StatusInternalServerError)
		return
	}

	settings := models.TelegramSettings{
		Key:               "telegram_config",
		BotTokenEncrypted: encToken,
		ChatIDEncrypted:   encChatID,
		IsConfigured:      true,
		UpdatedAt:         time.Now(),
	}

	_, err = s.mongoStore.Settings().UpdateOne(r.Context(),
		bson.M{"key": "telegram_config"},
		bson.M{"$set": settings},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update dispatcher credentials
	s.telegramDisp.SetCredentials(req.BotToken, req.ChatID)

	jsonResponse(w, map[string]string{"status": "saved"})
}

func (s *Server) handleTestTelegram(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BotToken string `json:"botToken"`
		ChatID   string `json:"chatId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if err := s.telegramDisp.SendTestMessage(req.BotToken, req.ChatID); err != nil {
		jsonError(w, "Telegram test failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	jsonResponse(w, map[string]string{"status": "sent"})
}

// ━━━ Helpers ━━━
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
