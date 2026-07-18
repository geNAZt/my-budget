package service

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"time"

	"github.com/genazt/my-budget-script/backend/pkg/apis/enablebanking"
	"github.com/golang-jwt/jwt/v5"
	"github.com/oapi-codegen/runtime/types"
)

type EnableBankingService struct {
}

func NewEnableBankingService() *EnableBankingService {
	return &EnableBankingService{}
}

func (s *EnableBankingService) getClient(ctx context.Context) (*enablebanking.ClientWithResponses, error) {
	httpClient := &http.Client{
		Transport: &AuditingTransport{
			Base: http.DefaultTransport,
		},
	}
	return enablebanking.NewClientWithResponses("https://api.enablebanking.com", enablebanking.WithHTTPClient(httpClient))
}

func (s *EnableBankingService) CreateJWT(applicationID string, privateKeyPEM string) (string, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", fmt.Errorf("failed to parse PEM block containing private key")
	}

	var priv *rsa.PrivateKey
	var err error

	if block.Type == "RSA PRIVATE KEY" {
		priv, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	} else if block.Type == "PRIVATE KEY" {
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return "", err
		}
		var ok bool
		priv, ok = key.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("not an RSA private key")
		}
	} else {
		return "", fmt.Errorf("unsupported key type: %s", block.Type)
	}

	if err != nil {
		return "", err
	}

	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": "enablebanking.com",
		"aud": "api.enablebanking.com",
		"iat": now.Unix(),
		"exp": now.Add(1 * time.Hour).Unix(),
	})

	token.Header["kid"] = applicationID

	return token.SignedString(priv)
}

func (s *EnableBankingService) GetASPSPs(ctx context.Context, token string, country string) ([]enablebanking.ASPSP, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	params := &enablebanking.GetAspspsParams{
		Country: &country,
	}

	resp, err := client.GetAspspsWithResponse(ctx, params, func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch ASPSPs (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	if resp.JSON200 == nil || resp.JSON200.Aspsps == nil {
		return []enablebanking.ASPSP{}, nil
	}

	return *resp.JSON200.Aspsps, nil
}

func (s *EnableBankingService) StartAuthorization(ctx context.Context, token string, bankName string, country string, redirectURL string, state string, psuID string) (string, string, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return "", "", err
	}

	now := time.Now()
	validUntil := now.AddDate(0, 0, 90)

	body := enablebanking.StartAuthorizationJSONRequestBody{
		Access: &struct {
			ValidUntil *time.Time `json:"valid_until,omitempty"`
		}{
			ValidUntil: &validUntil,
		},
		Aspsp: &struct {
			Country *string `json:"country,omitempty"`
			Name    *string `json:"name,omitempty"`
		}{
			Country: &country,
			Name:    &bankName,
		},
		RedirectUrl: &redirectURL,
		State:       &state,
		PsuId:       &psuID,
	}

	resp, err := client.StartAuthorizationWithResponse(ctx, body, func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return "", "", err
	}

	if resp.StatusCode() != http.StatusOK {
		return "", "", fmt.Errorf("failed to start auth (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	if resp.JSON200 == nil || resp.JSON200.Url == nil || resp.JSON200.AuthorizationId == nil {
		return "", "", fmt.Errorf("invalid response from start auth")
	}

	return *resp.JSON200.Url, *resp.JSON200.AuthorizationId, nil
}

func (s *EnableBankingService) CreateSession(ctx context.Context, token string, code string) (string, []string, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return "", nil, err
	}

	body := enablebanking.CreateSessionJSONRequestBody{
		Code: &code,
	}

	resp, err := client.CreateSessionWithResponse(ctx, body, func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return "", nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return "", nil, fmt.Errorf("failed to create session (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	if resp.JSON200 == nil || resp.JSON200.SessionId == nil {
		return "", nil, fmt.Errorf("invalid response from create session")
	}

	accounts := []string{}
	if resp.JSON200.Accounts != nil {
		accounts = s.extractAccountUIDs(*resp.JSON200.Accounts)
	}

	return *resp.JSON200.SessionId, accounts, nil
}

func (s *EnableBankingService) GetSession(ctx context.Context, token string, sessionID string) (string, []string, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return "", nil, err
	}

	resp, err := client.GetSessionWithResponse(ctx, sessionID, func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return "", nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return "", nil, fmt.Errorf("failed to fetch session (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	if resp.JSON200 == nil || resp.JSON200.Status == nil {
		return "", nil, fmt.Errorf("invalid response from get session")
	}

	accounts := []string{}
	if resp.JSON200.Accounts != nil {
		accounts = s.extractAccountUIDs(*resp.JSON200.Accounts)
	}

	return *resp.JSON200.Status, accounts, nil
}

func (s *EnableBankingService) extractAccountUIDs(refs []enablebanking.AccountReference) []string {
	uids := make([]string, 0, len(refs))
	for _, ref := range refs {
		if s0, err := ref.AsAccountReference0(); err == nil {
			uids = append(uids, s0)
		} else if s1, err := ref.AsAccountReference1(); err == nil && s1.Uid != nil {
			uids = append(uids, *s1.Uid)
		}
	}
	return uids
}

func (s *EnableBankingService) GetAccountDetails(ctx context.Context, token string, accountID string) (*enablebanking.Account, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := client.GetAccountDetailsWithResponse(ctx, accountID, func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch account details (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	return resp.JSON200, nil
}

func (s *EnableBankingService) GetBalances(ctx context.Context, token string, accountID string) ([]enablebanking.Balance, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := client.GetAccountBalancesWithResponse(ctx, accountID, func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch balances (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	if resp.JSON200 == nil || resp.JSON200.Balances == nil {
		return []enablebanking.Balance{}, nil
	}

	return *resp.JSON200.Balances, nil
}

func (s *EnableBankingService) GetTransactions(ctx context.Context, token string, accountID string, dateFrom string, psuHeaders map[string]string, strategy string) ([]enablebanking.Transaction, *http.Response, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, nil, err
	}

	var allTransactions []enablebanking.Transaction
	var continuationKey *string
	var lastResp *http.Response

	for {
		params := &enablebanking.GetAccountTransactionsParams{
			ContinuationKey: continuationKey,
		}

		if strategy != "" {
			params.Strategy = &strategy
		}

		if dateFrom != "" && continuationKey == nil {
			if t, err := time.Parse("2006-01-02", dateFrom); err == nil {
				params.DateFrom = &types.Date{Time: t}
			}
		}

		resp, err := client.GetAccountTransactionsWithResponse(ctx, accountID, params, func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			for k, v := range psuHeaders {
				req.Header.Set(k, v)
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}

		lastResp = resp.HTTPResponse

		if resp.StatusCode() != http.StatusOK {
			return nil, lastResp, fmt.Errorf("failed to fetch transactions (Status %d): %s", resp.StatusCode(), string(resp.Body))
		}

		if resp.JSON200 != nil && resp.JSON200.Transactions != nil {
			allTransactions = append(allTransactions, *resp.JSON200.Transactions...)
		}

		if resp.JSON200 == nil || resp.JSON200.ContinuationKey == nil || *resp.JSON200.ContinuationKey == "" {
			break
		}

		continuationKey = resp.JSON200.ContinuationKey
	}

	return allTransactions, lastResp, nil
}
