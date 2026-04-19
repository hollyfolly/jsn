// Package auth provides authentication management for ServiceNow.
package auth

import (
	"fmt"
	"os"
	"time"

	"github.com/jacebenson/jsn/internal/config"
)

const (
	serviceName = "servicenow"
)

// Manager handles authentication.
type Manager struct {
	cfg   *config.Config
	store *Store
}

// NewManager creates a new auth manager.
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		cfg:   cfg,
		store: NewStore(config.GlobalConfigDir()),
	}
}

// credentialKey returns the storage key for credentials.
// Uses the active profile's instance URL as the key.
func (m *Manager) credentialKey() string {
	if profile := m.cfg.GetActiveProfile(); profile != nil {
		return profile.InstanceURL
	}
	return ""
}

// GetCredentials retrieves credentials for the active profile.
// Checks SERVICENOW_TOKEN and SERVICENOW_OAUTH_TOKEN env vars first, then stored credentials.
func (m *Manager) GetCredentials() (*Credentials, error) {
	// Check for SERVICENOW_OAUTH_TOKEN environment variable first (OAuth)
	if token := os.Getenv("SERVICENOW_OAUTH_TOKEN"); token != "" {
		creds := &Credentials{
			AuthMethod:  "oauth",
			AccessToken: token,
			CreatedAt:   0,
		}
		// Optionally get refresh token from env
		if refresh := os.Getenv("SERVICENOW_OAUTH_REFRESH_TOKEN"); refresh != "" {
			creds.RefreshToken = refresh
		}
		return creds, nil
	}

	// Check for SERVICENOW_TOKEN environment variable (Basic Auth / g_ck)
	if token := os.Getenv("SERVICENOW_TOKEN"); token != "" {
		return &Credentials{
			Token:     token,
			CreatedAt: 0,
		}, nil
	}

	credKey := m.credentialKey()
	if credKey == "" {
		return nil, fmt.Errorf("no active profile configured")
	}

	creds, err := m.store.Load(credKey)
	if err != nil {
		return nil, err
	}

	// Auto-refresh OAuth token if needed
	if creds.IsOAuth() && creds.NeedsRefresh() && creds.RefreshToken != "" {
		profile := m.cfg.GetActiveProfile()
		if profile != nil {
			refreshed, err := m.RefreshOAuthToken(profile.InstanceURL, creds)
			if err == nil && refreshed != nil {
				return refreshed, nil
			}
			// If refresh fails, return original credentials (will fail on API call)
		}
	}

	return creds, nil
}

// StoreCredentials stores credentials for the active profile.
func (m *Manager) StoreCredentials(creds *Credentials) error {
	credKey := m.credentialKey()
	if credKey == "" {
		return fmt.Errorf("no active profile configured")
	}

	return m.store.Save(credKey, creds)
}

// DeleteCredentials removes credentials for the active profile.
func (m *Manager) DeleteCredentials() error {
	credKey := m.credentialKey()
	if credKey == "" {
		return fmt.Errorf("no active profile configured")
	}

	return m.store.Delete(credKey)
}

// IsAuthenticated checks if there are valid credentials for the active profile.
func (m *Manager) IsAuthenticated() bool {
	// Check for OAuth token in environment variable first
	if os.Getenv("SERVICENOW_OAUTH_TOKEN") != "" {
		return true
	}

	// Check for SERVICENOW_TOKEN environment variable (Basic Auth / g_ck)
	if os.Getenv("SERVICENOW_TOKEN") != "" {
		return true
	}

	credKey := m.credentialKey()
	if credKey == "" {
		return false
	}

	creds, err := m.store.Load(credKey)
	if err != nil {
		return false
	}
	// Check for OAuth or Basic Auth / g_ck tokens
	return creds.AccessToken != "" || creds.Token != ""
}

// GetStore returns the credential store.
func (m *Manager) GetStore() *Store {
	return m.store
}

// Credentials holds authentication tokens.
type Credentials struct {
	Token      string `json:"token"`
	Username   string `json:"username,omitempty"`
	Cookies    string `json:"cookies,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
	CreatedAt  int64  `json:"created_at"`
	LastTested int64  `json:"last_tested,omitempty"`
	// OAuth-specific fields
	AuthMethod   string `json:"auth_method,omitempty"` // "basic", "gck", or "oauth"
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
}

// IsOAuth returns true if these credentials use OAuth authentication
func (c *Credentials) IsOAuth() bool {
	return c.AuthMethod == "oauth" || c.AccessToken != ""
}

// IsExpired returns true if the OAuth token is expired or expires within the given buffer
func (c *Credentials) IsExpired(bufferSeconds int64) bool {
	if c.ExpiresAt == 0 {
		return false
	}
	return time.Now().Unix()+bufferSeconds >= c.ExpiresAt
}

// NeedsRefresh returns true if the OAuth token should be refreshed
// (expires within 15 minutes)
func (c *Credentials) NeedsRefresh() bool {
	if c.RefreshToken == "" {
		return false
	}
	return c.IsExpired(15 * 60)
}

// GetCredentialsForProfile retrieves credentials for a specific profile by instance URL.
// Checks SERVICENOW_TOKEN env var first only if this is the active profile.
func (m *Manager) GetCredentialsForProfile(instanceURL string) (*Credentials, error) {
	// Only check env var if this is the active profile
	if instanceURL == m.credentialKey() {
		if token := os.Getenv("SERVICENOW_TOKEN"); token != "" {
			return &Credentials{
				Token:     token,
				CreatedAt: 0,
			}, nil
		}
	}

	return m.store.Load(instanceURL)
}

// UpdateLastTested updates the last_tested timestamp for the active profile's credentials.
func (m *Manager) UpdateLastTested() error {
	credKey := m.credentialKey()
	if credKey == "" {
		return fmt.Errorf("no active profile configured")
	}

	creds, err := m.store.Load(credKey)
	if err != nil {
		return err
	}

	creds.LastTested = time.Now().Unix()
	return m.store.Save(credKey, creds)
}

// RefreshOAuthToken refreshes an OAuth access token using the refresh token
func (m *Manager) RefreshOAuthToken(instanceURL string, creds *Credentials) (*Credentials, error) {
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	clientID := GetOAuthClientID()
	tokenResp, err := RefreshAccessToken(instanceURL, clientID, creds.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	// Update credentials with new tokens
	creds.AccessToken = tokenResp.AccessToken
	creds.RefreshToken = tokenResp.RefreshToken
	creds.TokenType = tokenResp.TokenType
	creds.ExpiresAt = time.Now().Unix() + int64(tokenResp.ExpiresIn)

	// Store updated credentials
	credKey := m.credentialKey()
	if credKey != "" {
		if err := m.store.Save(credKey, creds); err != nil {
			// Log but don't fail - we still have valid credentials in memory
			fmt.Fprintf(os.Stderr, "warning: failed to store refreshed credentials: %v\n", err)
		}
	}

	return creds, nil
}
