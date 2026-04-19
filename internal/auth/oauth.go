// Package auth provides OAuth authentication support for ServiceNow.
package auth

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultOAuthClientID is the default ServiceNow OAuth client ID
// Users can override this with SERVICENOW_OAUTH_CLIENT_ID env var
const DefaultOAuthClientID = "543e5655f77746a28228c6009a599dfb"

// ServiceNowSDKRedirectURI is the redirect URI used by ServiceNow's SDK OAuth flow
// This is a special ServiceNow page that displays the authorization code for copying
const ServiceNowSDKRedirectURI = "/sdk-oauth.do"

// GetOAuthClientID returns the OAuth client ID to use
func GetOAuthClientID() string {
	if id := os.Getenv("SERVICENOW_OAUTH_CLIENT_ID"); id != "" {
		return id
	}
	return DefaultOAuthClientID
}

// PKCEParams holds the PKCE (Proof Key for Code Exchange) parameters
type PKCEParams struct {
	CodeVerifier  string
	CodeChallenge string
	State         string
}

// GeneratePKCE generates PKCE parameters for OAuth flow
func GeneratePKCE() (*PKCEParams, error) {
	// Generate code verifier (random 32 bytes, base64url encoded = 43 chars)
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return nil, fmt.Errorf("generating code verifier: %w", err)
	}
	codeVerifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	// Generate code challenge (SHA256 of verifier, base64url encoded)
	hash := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(hash[:])

	// Generate state parameter (random 16 bytes, base64url encoded)
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	return &PKCEParams{
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
		State:         state,
	}, nil
}

// BuildAuthorizationURL builds the ServiceNow OAuth authorization URL
// Using the SDK-style flow with /sdk-oauth.do redirect URI
func BuildAuthorizationURL(instanceURL string, clientID string, pkce *PKCEParams) string {
	u, _ := url.Parse(instanceURL)
	u.Path = "/oauth_auth.do"

	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", ServiceNowSDKRedirectURI)
	q.Set("state", pkce.State)
	q.Set("code_challenge", pkce.CodeChallenge)
	q.Set("code_challenge_method", "S256")
	q.Set("scope", "openid")
	u.RawQuery = q.Encode()

	return u.String()
}

// TokenResponse represents the OAuth token response from ServiceNow
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// ExchangeCodeForToken exchanges the authorization code for access/refresh tokens
func ExchangeCodeForToken(instanceURL, clientID, code string, pkce *PKCEParams) (*TokenResponse, error) {
	tokenURL := strings.TrimSuffix(instanceURL, "/") + "/oauth_token.do"

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", clientID)
	data.Set("code", code)
	data.Set("redirect_uri", ServiceNowSDKRedirectURI)
	data.Set("code_verifier", pkce.CodeVerifier)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchanging code for token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	return &tokenResp, nil
}

// RefreshAccessToken refreshes an OAuth access token using the refresh token
func RefreshAccessToken(instanceURL, clientID, refreshToken string) (*TokenResponse, error) {
	tokenURL := strings.TrimSuffix(instanceURL, "/") + "/oauth_token.do"

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", clientID)
	data.Set("refresh_token", refreshToken)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating refresh request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parsing refresh response: %w", err)
	}

	return &tokenResp, nil
}

// OAuthFlow handles the complete OAuth authentication flow using the SDK-style copy-paste method
func OAuthFlow(instanceURL string) (*Credentials, error) {
	clientID := GetOAuthClientID()

	// Generate PKCE parameters
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, fmt.Errorf("generating PKCE: %w", err)
	}

	// Build authorization URL using SDK-style redirect
	authURL := BuildAuthorizationURL(instanceURL, clientID, pkce)

	// Print instructions and open browser
	fmt.Printf("\n")
	fmt.Printf("Opening browser for OAuth authentication...\n")
	fmt.Printf("If the browser doesn't open automatically, visit:\n")
	fmt.Printf("%s\n\n", authURL)

	// Try to open browser
	_ = openBrowserCommand(authURL)

	// Prompt user for the authorization code
	fmt.Println("After authenticating in the browser, copy the authorization code shown on the page.")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	var authCode string
	for {
		fmt.Print("Authorization code: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading authorization code: %w", err)
		}
		authCode = strings.TrimSpace(input)
		if authCode != "" {
			break
		}
		fmt.Println("Authorization code is required.")
	}

	// Exchange code for tokens
	fmt.Println("\nExchanging authorization code for tokens...")
	tokenResp, err := ExchangeCodeForToken(instanceURL, clientID, authCode, pkce)
	if err != nil {
		return nil, fmt.Errorf("exchanging code: %w", err)
	}

	// Create credentials
	expiresAt := time.Now().Unix() + int64(tokenResp.ExpiresIn)
	creds := &Credentials{
		AuthMethod:   "oauth",
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    expiresAt,
		CreatedAt:    time.Now().Unix(),
	}

	return creds, nil
}
