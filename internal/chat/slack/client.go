package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// APIClient calls the small subset of Slack Web API endpoints used by chat configuration.
type APIClient struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

// NewAPIClient creates a Slack client with a bounded request timeout.
func NewAPIClient(token, baseURL string, httpClient *http.Client) (*APIClient, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("Slack bot token is required")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://slack.com/api"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &APIClient{token: token, baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}, nil
}

// GetChannelInfo returns the channel creator used for mutation authorization.
func (c *APIClient) GetChannelInfo(ctx context.Context, channelID string) (ChannelInfo, error) {
	var response apiResponse[ChannelInfo]
	err := c.call(ctx, "conversations.info", map[string]string{"channel": channelID}, &response)
	return response.Channel, err
}

// Respond sends an ephemeral or in-channel response through Slack's response URL.
func (c *APIClient) Respond(ctx context.Context, responseURL string, response Response) error {
	return c.postJSON(ctx, responseURL, response, false)
}

func (c *APIClient) call(ctx context.Context, method string, payload any, response any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	return c.do(request, response)
}

func (c *APIClient) postJSON(ctx context.Context, url string, payload any, bearer bool) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if bearer {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.do(request, nil)
}

func (c *APIClient) do(request *http.Request, result any) error {
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Slack API status %d", response.StatusCode)
	}
	if result != nil {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("decode Slack API response: %w", err)
		}
		if envelope, ok := result.(*apiResponse[ChannelInfo]); ok && !envelope.OK {
			return fmt.Errorf("Slack API: %s", envelope.Error)
		}
	}
	return nil
}
