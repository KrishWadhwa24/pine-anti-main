package store

import (
	"context"
	"time"

	"github.com/tradenexus/backend/internal/config"
	"github.com/tradenexus/backend/internal/logger"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDB collection names.
const (
	ColCandles              = "candles_ts"
	ColSignals              = "signals"
	ColIndicatorSnapshots   = "indicator_snapshots"
	ColWatchlists           = "watchlists"
	ColScannerMatches       = "scanner_matches"
	ColManualScannerResults = "manual_scanner_results"
	ColScannerRuns          = "scanner_runs"
	ColProcessingLedger     = "processing_ledger"
	ColSettings             = "settings"
	ColRecoveryCheckpoints  = "recovery_checkpoints"
)

// MongoStore wraps the MongoDB client and provides collection accessors.
type MongoStore struct {
	client *mongo.Client
	db     *mongo.Database
}

// NewMongoStore creates a new MongoDB connection and initializes collections and indexes.
func NewMongoStore(ctx context.Context, cfg *config.Config) (*MongoStore, error) {
	log := logger.WithComponent("mongo")

	clientOpts := options.Client().
		ApplyURI(cfg.MongoURI).
		SetServerSelectionTimeout(10 * time.Second).
		SetConnectTimeout(10 * time.Second)

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, err
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}

	log.Info().Str("uri", cfg.MongoURI).Str("db", cfg.MongoDatabase).Msg("MongoDB connected")

	store := &MongoStore{
		client: client,
		db:     client.Database(cfg.MongoDatabase),
	}

	if err := store.ensureIndexes(ctx); err != nil {
		log.Error().Err(err).Msg("Failed to create indexes")
		return nil, err
	}

	return store, nil
}

// Collection returns a named collection.
func (m *MongoStore) Collection(name string) *mongo.Collection {
	return m.db.Collection(name)
}

// Candles returns the candles time-series collection.
func (m *MongoStore) Candles() *mongo.Collection { return m.db.Collection(ColCandles) }

// Signals returns the signals collection.
func (m *MongoStore) Signals() *mongo.Collection { return m.db.Collection(ColSignals) }

// IndicatorSnapshots returns the indicator snapshots collection.
func (m *MongoStore) IndicatorSnapshots() *mongo.Collection {
	return m.db.Collection(ColIndicatorSnapshots)
}

// Watchlists returns the watchlists collection.
func (m *MongoStore) Watchlists() *mongo.Collection { return m.db.Collection(ColWatchlists) }

// ScannerMatches returns the scanner matches collection.
func (m *MongoStore) ScannerMatches() *mongo.Collection { return m.db.Collection(ColScannerMatches) }

// ManualScannerResults returns manual scanner preview results.
func (m *MongoStore) ManualScannerResults() *mongo.Collection {
	return m.db.Collection(ColManualScannerResults)
}

// ScannerRuns returns automatic scanner run ledger entries.
func (m *MongoStore) ScannerRuns() *mongo.Collection { return m.db.Collection(ColScannerRuns) }

// ProcessingLedger returns the processing ledger collection.
func (m *MongoStore) ProcessingLedger() *mongo.Collection {
	return m.db.Collection(ColProcessingLedger)
}

// Settings returns the settings collection.
func (m *MongoStore) Settings() *mongo.Collection { return m.db.Collection(ColSettings) }

// RecoveryCheckpoints returns the recovery checkpoints collection.
func (m *MongoStore) RecoveryCheckpoints() *mongo.Collection {
	return m.db.Collection(ColRecoveryCheckpoints)
}

// Close disconnects from MongoDB.
func (m *MongoStore) Close(ctx context.Context) error {
	return m.client.Disconnect(ctx)
}

// Ping checks MongoDB connectivity.
func (m *MongoStore) Ping(ctx context.Context) error {
	return m.client.Ping(ctx, nil)
}

// ensureIndexes creates all required compound indexes.
func (m *MongoStore) ensureIndexes(ctx context.Context) error {
	log := logger.WithComponent("mongo")

	// Candles: {symbol, timeframe, timestamp} unique
	_, err := m.Candles().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "symbol", Value: 1}, {Key: "timeframe", Value: 1}, {Key: "timestamp", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}

	// Signals: {signalHash} unique
	_, err = m.Signals().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "signalHash", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}

	// Signals: {symbol, timeframe, createdAt}
	_, err = m.Signals().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "symbol", Value: 1}, {Key: "timeframe", Value: 1}, {Key: "createdAt", Value: -1}},
	})
	if err != nil {
		return err
	}

	// IndicatorSnapshots: {symbol, timeframe} unique
	_, err = m.IndicatorSnapshots().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "symbol", Value: 1}, {Key: "timeframe", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}

	// Watchlists: {name} unique
	_, err = m.Watchlists().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "name", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}

	// ScannerMatches: {symbol, scannerType, weekTimestamp}
	_, err = m.ScannerMatches().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "symbol", Value: 1}, {Key: "scannerType", Value: 1}, {Key: "weekTimestamp", Value: 1}},
	})
	if err != nil {
		return err
	}

	// ManualScannerResults: {symbol, scannerType, weekTimestamp}
	_, err = m.ManualScannerResults().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "symbol", Value: 1}, {Key: "scannerType", Value: 1}, {Key: "weekTimestamp", Value: 1}},
	})
	if err != nil {
		return err
	}

	// ScannerRuns: {symbol, scannerMode, weekTimestamp}
	_, err = m.ScannerRuns().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "symbol", Value: 1}, {Key: "scannerMode", Value: 1}, {Key: "weekTimestamp", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}

	// ProcessingLedger: {symbol, timeframe} unique
	_, err = m.ProcessingLedger().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "symbol", Value: 1}, {Key: "timeframe", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}

	// Settings: {key} unique
	_, err = m.Settings().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "key", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}

	// RecoveryCheckpoints: {checkpointType} unique
	_, err = m.RecoveryCheckpoints().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "checkpointType", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}

	log.Info().Msg("MongoDB indexes ensured")
	return nil
}
