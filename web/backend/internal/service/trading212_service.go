package service

import (
	"context"
	"fmt"
	"net/http"

	"github.com/genazt/my-budget-script/web/backend/pkg/apis/trading212"
)

type Trading212Service struct {
}

func NewTrading212Service() *Trading212Service {
	return &Trading212Service{}
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
		return nil, fmt.Errorf("T212 API error: %d - %s", resp.StatusCode(), string(resp.Body))
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
		return nil, fmt.Errorf("T212 API error: %d - %s", resp.StatusCode(), string(resp.Body))
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
		return nil, fmt.Errorf("T212 API error: %d - %s", resp.StatusCode(), string(resp.Body))
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
		return nil, fmt.Errorf("T212 API error: %d - %s", resp.StatusCode(), string(resp.Body))
	}

	if resp.JSON200 == nil {
		return []trading212.Order{}, nil
	}

	return *resp.JSON200, nil
}
