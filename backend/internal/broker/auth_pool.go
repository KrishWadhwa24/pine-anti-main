package broker

import (
	"sync/atomic"

	"github.com/tradenexus/backend/internal/config"
	"github.com/tradenexus/backend/internal/logger"
)

// AuthPool manages a pool of AuthManager instances for round-robin load distribution.
// This distributes Angel One API calls across multiple credential sets to avoid rate limits.
type AuthPool struct {
	managers []*AuthManager
	index    uint64 // atomic round-robin counter
}

// NewAuthPool creates an auth pool from the config's credential slots.
// It auto-discovers which slots (1, 2, 3) are configured and only includes those.
// Falls back to a single-manager pool if only the primary credential is configured.
func NewAuthPool(cfg *config.Config) *AuthPool {
	log := logger.WithComponent("broker.auth_pool")

	pool := &AuthPool{}

	// Slot 1 — primary credential
	cred1 := AngelCredential{
		SlotID:     "slot-1",
		APIKey:     cfg.AngelAPIKey,
		ClientID:   cfg.AngelClientID,
		Password:   cfg.AngelPassword,
		TOTPSecret: cfg.AngelTOTPSecret,
	}
	if cred1.IsConfigured() {
		pool.managers = append(pool.managers, NewAuthManager(cred1))
		log.Info().Str("slot", "slot-1").Str("clientID", cfg.AngelClientID).Msg("Credential slot configured")
	}

	// Slot 2
	cred2 := AngelCredential{
		SlotID:     "slot-2",
		APIKey:     cfg.AngelAPIKey2,
		ClientID:   cfg.AngelClientID2,
		Password:   cfg.AngelPassword2,
		TOTPSecret: cfg.AngelTOTPSecret2,
	}
	if cred2.IsConfigured() {
		pool.managers = append(pool.managers, NewAuthManager(cred2))
		log.Info().Str("slot", "slot-2").Str("clientID", cfg.AngelClientID2).Msg("Credential slot configured")
	}

	// Slot 3
	cred3 := AngelCredential{
		SlotID:     "slot-3",
		APIKey:     cfg.AngelAPIKey3,
		ClientID:   cfg.AngelClientID3,
		Password:   cfg.AngelPassword3,
		TOTPSecret: cfg.AngelTOTPSecret3,
	}
	if cred3.IsConfigured() {
		pool.managers = append(pool.managers, NewAuthManager(cred3))
		log.Info().Str("slot", "slot-3").Str("clientID", cfg.AngelClientID3).Msg("Credential slot configured")
	}

	log.Info().Int("totalSlots", len(pool.managers)).Msg("Auth pool initialized")
	return pool
}

// Size returns the number of configured credential slots in the pool.
func (p *AuthPool) Size() int {
	return len(p.managers)
}

// HasCredentials returns true if at least one credential slot is configured.
func (p *AuthPool) HasCredentials() bool {
	return len(p.managers) > 0
}

// Primary returns the first (primary) AuthManager, used for WebSocket connections.
// Returns nil if no credentials are configured.
func (p *AuthPool) Primary() *AuthManager {
	if len(p.managers) == 0 {
		return nil
	}
	return p.managers[0]
}

// Next returns the next AuthManager in round-robin order.
// This distributes API calls evenly across all configured credential slots.
// Returns nil if no credentials are configured.
func (p *AuthPool) Next() *AuthManager {
	if len(p.managers) == 0 {
		return nil
	}
	idx := atomic.AddUint64(&p.index, 1)
	return p.managers[idx%uint64(len(p.managers))]
}

// NextHistoricalClient creates a new HistoricalClient using the next credential in round-robin.
// Returns nil if no credentials are configured.
func (p *AuthPool) NextHistoricalClient() *HistoricalClient {
	auth := p.Next()
	if auth == nil {
		return nil
	}
	return NewHistoricalClient(auth)
}
