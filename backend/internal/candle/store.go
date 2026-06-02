package candle

import (
	"context"
	"time"

	"github.com/tradenexus/backend/internal/logger"
	"github.com/tradenexus/backend/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Store handles candle persistence to MongoDB.
type Store struct {
	collection *mongo.Collection
}

// NewStore creates a new candle store.
func NewStore(collection *mongo.Collection) *Store {
	return &Store{collection: collection}
}

// SaveCandle upserts a finalized candle to MongoDB.
func (s *Store) SaveCandle(ctx context.Context, candle models.Candle) error {
	filter := bson.M{
		"symbol":    candle.Symbol,
		"timeframe": candle.Timeframe,
		"timestamp": candle.Timestamp,
	}

	update := bson.M{"$set": candle}
	opts := options.Update().SetUpsert(true)

	_, err := s.collection.UpdateOne(ctx, filter, update, opts)
	return err
}

// SaveCandlesBulk bulk-inserts finalized candles, skipping duplicates.
func (s *Store) SaveCandlesBulk(ctx context.Context, candles []models.Candle) error {
	if len(candles) == 0 {
		return nil
	}

	var ops []mongo.WriteModel
	for _, c := range candles {
		filter := bson.M{
			"symbol":    c.Symbol,
			"timeframe": c.Timeframe,
			"timestamp": c.Timestamp,
		}
		update := bson.M{"$setOnInsert": c}
		op := mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true)
		ops = append(ops, op)
	}

	_, err := s.collection.BulkWrite(ctx, ops, options.BulkWrite().SetOrdered(false))
	return err
}

// GetCandles retrieves finalized candles for a symbol and timeframe within a time range.
func (s *Store) GetCandles(ctx context.Context, symbol, timeframe string, from, to time.Time) ([]models.Candle, error) {
	filter := bson.M{
		"symbol":    symbol,
		"timeframe": timeframe,
		"timestamp": bson.M{"$gte": from, "$lte": to},
		"finalized": true,
	}

	opts := options.Find().SetSort(bson.M{"timestamp": 1})
	cursor, err := s.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var candles []models.Candle
	if err := cursor.All(ctx, &candles); err != nil {
		return nil, err
	}
	return candles, nil
}

// GetLastCandle retrieves the most recent finalized candle for a symbol and timeframe.
func (s *Store) GetLastCandle(ctx context.Context, symbol, timeframe string) (*models.Candle, error) {
	filter := bson.M{
		"symbol":    symbol,
		"timeframe": timeframe,
		"finalized": true,
	}

	opts := options.FindOne().SetSort(bson.M{"timestamp": -1})
	var candle models.Candle
	err := s.collection.FindOne(ctx, filter, opts).Decode(&candle)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &candle, nil
}

// GetRecentCandles retrieves the N most recent finalized candles.
func (s *Store) GetRecentCandles(ctx context.Context, symbol, timeframe string, limit int) ([]models.Candle, error) {
	filter := bson.M{
		"symbol":    symbol,
		"timeframe": timeframe,
		"finalized": true,
	}

	opts := options.Find().SetSort(bson.M{"timestamp": -1}).SetLimit(int64(limit))
	cursor, err := s.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var candles []models.Candle
	if err := cursor.All(ctx, &candles); err != nil {
		return nil, err
	}

	// Reverse to chronological order
	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}
	return candles, nil
}

// CountCandles returns the number of finalized candles for a symbol+timeframe.
func (s *Store) CountCandles(ctx context.Context, symbol, timeframe string) (int64, error) {
	filter := bson.M{
		"symbol":    symbol,
		"timeframe": timeframe,
		"finalized": true,
	}
	return s.collection.CountDocuments(ctx, filter)
}

// DetectGaps finds missing candle timestamps in a sequence.
func (s *Store) DetectGaps(ctx context.Context, symbol, timeframe string, from, to time.Time) ([]time.Time, error) {
	log := logger.WithComponent("candle.store")

	candles, err := s.GetCandles(ctx, symbol, timeframe, from, to)
	if err != nil {
		return nil, err
	}

	if len(candles) == 0 {
		return nil, nil
	}

	var gaps []time.Time
	for i := 1; i < len(candles); i++ {
		expected := expectedNextTimestamp(candles[i-1].Timestamp, timeframe)
		if !candles[i].Timestamp.Equal(expected) && candles[i].Timestamp.After(expected) {
			gaps = append(gaps, expected)
			log.Warn().
				Str("symbol", symbol).
				Str("tf", timeframe).
				Time("missing", expected).
				Msg("Candle gap detected")
		}
	}

	return gaps, nil
}

// PruneOldCandles removes old candles beyond the specified retention limit for a symbol+timeframe.
func (s *Store) PruneOldCandles(ctx context.Context, symbol, timeframe string, retainLimit int) (int64, error) {
	filter := bson.M{
		"symbol":    symbol,
		"timeframe": timeframe,
	}

	opts := options.Find().SetSort(bson.M{"timestamp": -1}).SetSkip(int64(retainLimit)).SetLimit(1)
	cursor, err := s.collection.Find(ctx, filter, opts)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	var cutoffCandle models.Candle
	if cursor.Next(ctx) {
		if err := cursor.Decode(&cutoffCandle); err != nil {
			return 0, err
		}
	} else {
		// No candles beyond the limit
		return 0, nil
	}

	// Delete all candles for this symbol+tf older than or equal to the cutoff candle's timestamp
	deleteFilter := bson.M{
		"symbol":    symbol,
		"timeframe": timeframe,
		"timestamp": bson.M{"$lte": cutoffCandle.Timestamp},
	}
	res, err := s.collection.DeleteMany(ctx, deleteFilter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

func expectedNextTimestamp(current time.Time, timeframe string) time.Time {
	switch timeframe {
	case models.Timeframe1H:
		return current.Add(1 * time.Hour)
	case models.Timeframe4H:
		// Indian market 4H boundaries: 9:15-13:15, 13:15-15:30
		// If current is the first boundary (9:15), next is 13:15 same day
		// If current is the second boundary (13:15), next is 9:15 next trading day
		ct := current.In(IST)
		firstBoundary := time.Date(ct.Year(), ct.Month(), ct.Day(), 9, 15, 0, 0, IST)
		secondBoundary := time.Date(ct.Year(), ct.Month(), ct.Day(), 13, 15, 0, 0, IST)
		if ct.Equal(firstBoundary) || ct.Before(secondBoundary) {
			return secondBoundary
		}
		// Next is 9:15 on the next weekday
		next := ct.AddDate(0, 0, 1)
		for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
			next = next.AddDate(0, 0, 1)
		}
		return time.Date(next.Year(), next.Month(), next.Day(), 9, 15, 0, 0, IST)
	case models.Timeframe1D:
		return current.AddDate(0, 0, 1)
	case models.Timeframe1W:
		return current.AddDate(0, 0, 7)
	case models.Timeframe1M:
		return current.AddDate(0, 1, 0)
	default:
		return current.Add(1 * time.Hour)
	}
}
