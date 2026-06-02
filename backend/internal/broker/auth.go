package broker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/tradenexus/backend/internal/logger"
)

const (
	baseURL       = "https://apiconnect.angelone.in"
	loginEndpoint = "/rest/auth/angelbroking/user/v1/loginByPassword"
	tokenRefresh  = "/rest/auth/angelbroking/jwt/v1/generateTokens"
)

// AngelCredential holds a single set of Angel One SmartAPI credentials.
type AngelCredential struct {
	SlotID     string // "slot-1", "slot-2", "slot-3" for logging
	APIKey     string
	ClientID   string
	Password   string
	TOTPSecret string
}

// IsConfigured returns true if the credential has all required fields.
func (c AngelCredential) IsConfigured() bool {
	return c.APIKey != "" && c.ClientID != "" && c.Password != "" && c.TOTPSecret != ""
}

// AuthManager handles Angel One SmartAPI JWT + TOTP authentication.
type AuthManager struct {
	cred       AngelCredential
	httpClient *http.Client

	mu        sync.RWMutex
	jwtToken  string
	feedToken string
	expiresAt time.Time
}

// LoginResponse represents the Angel One login API response.
type LoginResponse struct {
	Status    bool   `json:"status"`
	Message   string `json:"message"`
	ErrorCode string `json:"errorcode"`
	Data      struct {
		JWTToken     string `json:"jwtToken"`
		RefreshToken string `json:"refreshToken"`
		FeedToken    string `json:"feedToken"`
	} `json:"data"`
}

// NewAuthManager creates a new auth manager from a credential set.
func NewAuthManager(cred AngelCredential) *AuthManager {
	return &AuthManager{
		cred: cred,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Login performs the TOTP-based login flow and stores JWT + feed tokens.
func (a *AuthManager) Login() error {
	log := logger.WithComponent("broker.auth")

	// Generate TOTP code from secret
	totpCode, err := totp.GenerateCode(a.cred.TOTPSecret, time.Now())
	if err != nil {
		log.Error().Err(err).Str("slot", a.cred.SlotID).Msg("Failed to generate TOTP code")
		return fmt.Errorf("TOTP generation failed: %w", err)
	}

	payload := map[string]string{
		"clientcode": a.cred.ClientID,
		"password":   a.cred.Password,
		"totp":       totpCode,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", baseURL+loginEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-UserType", "USER")
	req.Header.Set("X-SourceID", "WEB")
	req.Header.Set("X-ClientLocalIP", "127.0.0.1")
	req.Header.Set("X-ClientPublicIP", "127.0.0.1")
	req.Header.Set("X-MACAddress", "00:00:00:00:00:00")
	req.Header.Set("X-PrivateKey", a.cred.APIKey)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		log.Error().Err(err).Str("slot", a.cred.SlotID).Msg("Login request failed")
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var loginResp LoginResponse
	if err := json.Unmarshal(respBody, &loginResp); err != nil {
		return fmt.Errorf("failed to parse login response: %w", err)
	}

	if !loginResp.Status {
		return fmt.Errorf("login failed [%s]: %s (code: %s)", a.cred.SlotID, loginResp.Message, loginResp.ErrorCode)
	}

	a.mu.Lock()
	a.jwtToken = loginResp.Data.JWTToken
	a.feedToken = loginResp.Data.FeedToken
	a.expiresAt = time.Now().Add(23 * time.Hour) // JWT valid ~24h
	a.mu.Unlock()

	log.Info().Str("slot", a.cred.SlotID).Msg("Angel One login successful")
	return nil
}

// GetJWTToken returns the current JWT token, refreshing if expired.
func (a *AuthManager) GetJWTToken() (string, error) {
	a.mu.RLock()
	token := a.jwtToken
	expired := time.Now().After(a.expiresAt)
	a.mu.RUnlock()

	if token == "" || expired {
		if err := a.Login(); err != nil {
			return "", err
		}
		a.mu.RLock()
		token = a.jwtToken
		a.mu.RUnlock()
	}
	return token, nil
}

// GetFeedToken returns the current feed token.
func (a *AuthManager) GetFeedToken() (string, error) {
	a.mu.RLock()
	token := a.feedToken
	a.mu.RUnlock()

	if token == "" {
		if err := a.Login(); err != nil {
			return "", err
		}
		a.mu.RLock()
		token = a.feedToken
		a.mu.RUnlock()
	}
	return token, nil
}

// GetAPIKey returns the API key from the credential.
func (a *AuthManager) GetAPIKey() string {
	return a.cred.APIKey
}

// GetClientCode returns the client code from the credential.
func (a *AuthManager) GetClientCode() string {
	return a.cred.ClientID
}

// GetSlotID returns the credential slot identifier.
func (a *AuthManager) GetSlotID() string {
	return a.cred.SlotID
}

// AuthHeaders returns the standard authenticated headers for REST API calls.
func (a *AuthManager) AuthHeaders() (map[string]string, error) {
	jwt, err := a.GetJWTToken()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"Authorization":    "Bearer " + jwt,
		"Content-Type":     "application/json",
		"Accept":           "application/json",
		"X-UserType":       "USER",
		"X-SourceID":       "WEB",
		"X-ClientLocalIP":  "127.0.0.1",
		"X-ClientPublicIP": "127.0.0.1",
		"X-MACAddress":     "00:00:00:00:00:00",
		"X-PrivateKey":     a.cred.APIKey,
	}, nil
}
