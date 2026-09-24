package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/genazt/my-budget-script/backend/pkg/apis/trading212"
)

type Trading212Service struct {
}

func NewTrading212Service() *Trading212Service {
	return &Trading212Service{}
}

func (s *Trading212Service) ExtractRateLimit(resp *http.Response) *time.Time {
	if resp == nil {
		return nil
	}

	retryAfter := getHeader(resp.Header, "retry-after", "Retry-After")
	if retryAfter != "" {
		if t := ParseResetTime(retryAfter); t != nil {
			return t
		}
	}

	reset := getHeader(resp.Header, "x-ratelimit-reset", "X-RateLimit-Reset", "RateLimit-Reset")
	remaining := getHeader(resp.Header, "x-ratelimit-remaining", "X-RateLimit-Remaining", "RateLimit-Remaining")

	if resp.StatusCode == http.StatusTooManyRequests {
		if reset != "" {
			if t := ParseResetTime(reset); t != nil {
				return t
			}
		}
		fallback := time.Now().Add(60 * time.Second)
		return &fallback
	}

	if remaining != "" && reset != "" {
		rem, err := strconv.Atoi(remaining)
		if err == nil && rem <= 0 {
			return ParseResetTime(reset)
		}
	}

	return nil
}

func (s *Trading212Service) getClient(ctx context.Context, apiKey, apiSecret string) (*trading212.ClientWithResponses, error) {
	httpClient := &http.Client{
		Transport: &AuditingTransport{
			Base: http.DefaultTransport,
		},
	}
	return trading212.NewClientWithResponses("https://live.trading212.com", trading212.WithHTTPClient(httpClient), trading212.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
		req.SetBasicAuth(apiKey, apiSecret)
		return nil
	}))
}

func (s *Trading212Service) parseResponseError(statusCode int, body []byte, httpResp *http.Response) error {
	if statusCode == http.StatusTooManyRequests {
		retryAfter := time.Now().Add(60 * time.Second)
		if bu := s.ExtractRateLimit(httpResp); bu != nil {
			retryAfter = *bu
		}
		return &RateLimitError{
			RetryAfter: retryAfter,
			Message:    fmt.Sprintf("T212 rate limit exceeded (429): %s", string(body)),
		}
	}
	return fmt.Errorf("T212 API error: %d - %s", statusCode, string(body))
}

func (s *Trading212Service) GetAccountSummary(ctx context.Context, apiKey, apiSecret string) (*trading212.AccountSummary, error) {
	client, err := s.getClient(ctx, apiKey, apiSecret)
	if err != nil {
		return nil, err
	}

	resp, err := client.GetAccountSummaryWithResponse(ctx)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, s.parseResponseError(resp.StatusCode(), resp.Body, resp.HTTPResponse)
	}

	return resp.JSON200, nil
}

func (s *Trading212Service) GetPositions(ctx context.Context, apiKey, apiSecret string) ([]trading212.Position, error) {
	client, err := s.getClient(ctx, apiKey, apiSecret)
	if err != nil {
		return nil, err
	}

	resp, err := client.GetPositionsWithResponse(ctx, nil)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, s.parseResponseError(resp.StatusCode(), resp.Body, resp.HTTPResponse)
	}

	if resp.JSON200 == nil {
		return []trading212.Position{}, nil
	}

	return *resp.JSON200, nil
}

func (s *Trading212Service) GetTransactions(ctx context.Context, apiKey, apiSecret string, limit int, cursor string) (*trading212.TransactionsResponse, error) {
	client, err := s.getClient(ctx, apiKey, apiSecret)
	if err != nil {
		return nil, err
	}

	l := int32(limit)
	params := &trading212.TransactionsParams{
		Limit: &l,
	}
	if cursor != "" {
		params.Cursor = &cursor
	}

	resp, err := client.TransactionsWithResponse(ctx, params)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, s.parseResponseError(resp.StatusCode(), resp.Body, resp.HTTPResponse)
	}

	return resp, nil
}

func (s *Trading212Service) GetActiveOrders(ctx context.Context, apiKey, apiSecret string) ([]trading212.Order, error) {
	client, err := s.getClient(ctx, apiKey, apiSecret)
	if err != nil {
		return nil, err
	}

	resp, err := client.OrdersWithResponse(ctx)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, s.parseResponseError(resp.StatusCode(), resp.Body, resp.HTTPResponse)
	}

	if resp.JSON200 == nil {
		return []trading212.Order{}, nil
	}

	return *resp.JSON200, nil
}
