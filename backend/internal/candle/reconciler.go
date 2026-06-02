package candle

import "time"

// Reconciler periodically verifies candle continuity and backfills gaps.
type Reconciler struct {
	store *Store
}

// NewReconciler creates a new candle reconciler.
func NewReconciler(store *Store) *Reconciler {
	return &Reconciler{store: store}
}

// GetWeekStartExported is an exported version of getWeekStart for use by other packages.
func GetWeekStartExported(t time.Time) time.Time {
	return getWeekStart(t)
}
