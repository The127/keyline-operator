package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
)

type keylineClient struct {
	baseURL    string
	httpClient *http.Client
}

func newKeylineClient(baseURL string, ts oauth2.TokenSource) *keylineClient {
	return &keylineClient{
		baseURL:    baseURL,
		httpClient: oauth2.NewClient(context.Background(), ts),
	}
}

type virtualServerState struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	DisplayName              string `json:"displayName"`
	RegistrationEnabled      bool   `json:"registrationEnabled"`
	Require2FA               bool   `json:"require2fa"`
	RequireEmailVerification bool   `json:"requireEmailVerification"`
}

type patchVirtualServerRequest struct {
	DisplayName              *string `json:"displayName,omitempty"`
	EnableRegistration       *bool   `json:"enableRegistration,omitempty"`
	Require2FA               *bool   `json:"require2fa,omitempty"`
	RequireEmailVerification *bool   `json:"requireEmailVerification,omitempty"`
}

func (c *keylineClient) getVirtualServer(ctx context.Context, name string) (*virtualServerState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/virtual-servers/%s", c.baseURL, name), nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get virtual server: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get virtual server returned %d: %s", resp.StatusCode, body)
	}

	var state virtualServerState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return nil, fmt.Errorf("decoding virtual server response: %w", err)
	}
	return &state, nil
}

func (c *keylineClient) patchVirtualServer(ctx context.Context, name string, patch patchVirtualServerRequest) error {
	body, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("encoding patch: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch,
		fmt.Sprintf("%s/api/virtual-servers/%s", c.baseURL, name),
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("patch virtual server: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("patch virtual server returned %d: %s", resp.StatusCode, body)
	}
	return nil
}
