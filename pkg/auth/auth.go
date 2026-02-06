// Package auth provides P3 token validation and user authentication.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// TokenValidator validates P3 authentication tokens.
type TokenValidator struct {
	userServiceURL string
	workspaceURL   string
	httpClient     *http.Client
}

// NewTokenValidator creates a new token validator.
func NewTokenValidator(userServiceURL, workspaceURL string) *TokenValidator {
	return &TokenValidator{
		userServiceURL: userServiceURL,
		workspaceURL:   workspaceURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// UserInfo contains validated user information.
type UserInfo struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Token    string `json:"-"`
}

// ValidateToken validates a P3 token and returns user information.
func (tv *TokenValidator) ValidateToken(ctx context.Context, token string) (*UserInfo, error) {
	if token == "" {
		return nil, fmt.Errorf("empty token")
	}

	// Parse the token to extract basic info
	// P3 tokens are typically in format: user@domain|token_id|...
	parts := strings.Split(token, "|")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid token format")
	}

	userID := parts[0]

	// Validate token against user service
	req, err := http.NewRequestWithContext(ctx, "GET", tv.userServiceURL+"/user/"+userID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", token)
	req.Header.Set("Accept", "application/json")

	resp, err := tv.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to validate token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("invalid or expired token")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token validation failed with status: %d", resp.StatusCode)
	}

	var userResp struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Email    string `json:"email"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&userResp); err != nil {
		return nil, fmt.Errorf("failed to parse user response: %w", err)
	}

	return &UserInfo{
		UserID:   userResp.ID,
		Username: userResp.Username,
		Email:    userResp.Email,
		Token:    token,
	}, nil
}

// ValidateWorkspaceAccess checks if the user has access to a Workspace path.
func (tv *TokenValidator) ValidateWorkspaceAccess(ctx context.Context, token, path string) error {
	// Construct Workspace API request to check access
	req, err := http.NewRequestWithContext(ctx, "GET", tv.workspaceURL+"/stat"+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", token)
	req.Header.Set("Accept", "application/json")

	resp, err := tv.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to check workspace access: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized access to workspace path: %s", path)
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("workspace path not found: %s", path)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("workspace access check failed with status: %d", resp.StatusCode)
	}

	return nil
}

// ExtractToken extracts the token from an HTTP request.
func ExtractToken(r *http.Request) string {
	// Check Authorization header
	auth := r.Header.Get("Authorization")
	if auth != "" {
		// Handle "Bearer " prefix if present
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
		return auth
	}

	// Check X-Auth-Token header (common in BV-BRC)
	if token := r.Header.Get("X-Auth-Token"); token != "" {
		return token
	}

	// Check query parameter
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}

	return ""
}

// ContextKey is the type for context keys.
type ContextKey string

// UserContextKey is the context key for user information.
const UserContextKey ContextKey = "user"

// GetUserFromContext retrieves user information from context.
func GetUserFromContext(ctx context.Context) *UserInfo {
	if user, ok := ctx.Value(UserContextKey).(*UserInfo); ok {
		return user
	}
	return nil
}

// SetUserInContext sets user information in context.
func SetUserInContext(ctx context.Context, user *UserInfo) context.Context {
	return context.WithValue(ctx, UserContextKey, user)
}

// ServiceAuth provides service-level authentication.
type ServiceAuth struct {
	serviceToken string
}

// NewServiceAuth creates a new service authenticator.
func NewServiceAuth(serviceToken string) *ServiceAuth {
	return &ServiceAuth{
		serviceToken: serviceToken,
	}
}

// GetServiceToken returns the service token for API calls.
func (sa *ServiceAuth) GetServiceToken() string {
	return sa.serviceToken
}

// AddAuthHeader adds the service token to an HTTP request.
func (sa *ServiceAuth) AddAuthHeader(req *http.Request) {
	req.Header.Set("Authorization", sa.serviceToken)
}
