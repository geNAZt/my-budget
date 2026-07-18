package service

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/genazt/my-budget-script/backend/pkg/apis/gocardless"
	"github.com/google/uuid"
	"github.com/oapi-codegen/runtime/types"
)

type GoCardlessService struct {
}

func NewGoCardlessService() *GoCardlessService {
	return &GoCardlessService{}
}

func (s *GoCardlessService) getClient(ctx context.Context) (*gocardless.ClientWithResponses, error) {
	httpClient := &http.Client{
		Transport: &AuditingTransport{
			Base: http.DefaultTransport,
		},
	}
	return gocardless.NewClientWithResponses("https://bankaccountdata.gocardless.com", gocardless.WithHTTPClient(httpClient))
}

func (s *GoCardlessService) GetAccessToken(ctx context.Context, id, key string) (string, *http.Response, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return "", nil, err
	}

	resp, err := client.ObtainNewAccessrefreshTokenPairWithResponse(ctx, gocardless.JWTObtainPairRequest{
		SecretId:  id,
		SecretKey: key,
	})
	if err != nil {
		return "", nil, err
	}

	if resp.StatusCode() == http.StatusTooManyRequests {
		return "", resp.HTTPResponse, s.parseRateLimitError(resp.Body)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", resp.HTTPResponse, fmt.Errorf("gocardless auth failed (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	if resp.JSON200 == nil || resp.JSON200.Access == nil {
		return "", resp.HTTPResponse, fmt.Errorf("gocardless auth response missing access token")
	}

	return *resp.JSON200.Access, resp.HTTPResponse, nil
}

func (s *GoCardlessService) GetTransactions(ctx context.Context, accountID string, token string, dateFrom string) (*gocardless.AccountTransactions, *http.Response, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, nil, err
	}

	params := &gocardless.RetrieveAccountTransactionsParams{}
	if dateFrom != "" {
		if t, err := time.Parse("2006-01-02", dateFrom); err == nil {
			params.DateFrom = &types.Date{Time: t}
		}
	}

	resp, err := client.RetrieveAccountTransactionsWithResponse(ctx, accountID, params, func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode() == http.StatusTooManyRequests {
		return nil, resp.HTTPResponse, s.parseRateLimitError(resp.Body)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, resp.HTTPResponse, fmt.Errorf("failed to fetch transactions (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	return resp.JSON200, resp.HTTPResponse, nil
}

func (s *GoCardlessService) GetRequisition(ctx context.Context, requisitionID string, token string) (*gocardless.Requisition, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	uid, err := uuid.Parse(requisitionID)
	if err != nil {
		return nil, err
	}

	resp, err := client.RequisitionByIdWithResponse(ctx, uid, func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() == http.StatusTooManyRequests {
		return nil, s.parseRateLimitError(resp.Body)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch requisition (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	return resp.JSON200, nil
}

func (s *GoCardlessService) CreateRequisition(ctx context.Context, institutionID string, redirectURL string, token string) (*gocardless.SpectacularRequisition, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := client.CreateRequisitionWithResponse(ctx, gocardless.CreateRequisitionJSONRequestBody{
		InstitutionId: institutionID,
		Redirect:      &redirectURL,
	}, func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() == http.StatusTooManyRequests {
		return nil, s.parseRateLimitError(resp.Body)
	}

	if resp.StatusCode() != http.StatusCreated && resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to create requisition (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	return resp.JSON201, nil
}

func (s *GoCardlessService) GetInstitutions(ctx context.Context, country string, token string) ([]gocardless.Integration, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	params := &gocardless.RetrieveAllSupportedInstitutionsInAGivenCountryParams{
		Country: &country,
	}

	resp, err := client.RetrieveAllSupportedInstitutionsInAGivenCountryWithResponse(ctx, params, func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() == http.StatusTooManyRequests {
		return nil, s.parseRateLimitError(resp.Body)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch institutions (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	if resp.JSON200 == nil {
		return []gocardless.Integration{}, nil
	}

	return *resp.JSON200, nil
}

func (s *GoCardlessService) GetAccountDetails(ctx context.Context, accountID string, token string) (*gocardless.AccountDetail, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := client.RetrieveAccountDetailsWithResponse(ctx, accountID, func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() == http.StatusTooManyRequests {
		return nil, s.parseRateLimitError(resp.Body)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch account details (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	return resp.JSON200, nil
}

func (s *GoCardlessService) GetBalances(ctx context.Context, accountID string, token string) (*gocardless.AccountBalance, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := client.RetrieveAccountBalancesWithResponse(ctx, accountID, func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() == http.StatusTooManyRequests {
		return nil, s.parseRateLimitError(resp.Body)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch balances (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	return resp.JSON200, nil
}

func (s *GoCardlessService) ExtractRateLimit(resp *http.Response) *time.Time {
	if resp == nil {
		return nil
	}

	remaining := resp.Header.Get("HTTP_X_RATELIMIT_REMAINING")
	reset := resp.Header.Get("HTTP_X_RATELIMIT_RESET")

	if remaining == "" || reset == "" {
		return nil
	}

	rem, _ := strconv.Atoi(remaining)
	res, _ := strconv.ParseInt(reset, 10, 64)

	// GoCardless is usually strict, if we have 0 or 1 left, back off until reset
	if rem <= 1 {
		t := time.Unix(res, 0)
		return &t
	}

	return nil
}

func (s *GoCardlessService) parseRateLimitError(body []byte) error {
	errMsg := string(body)

	waitTime := 24 * time.Hour
	re := regexp.MustCompile(`in (\d+) seconds`)
	matches := re.FindStringSubmatch(errMsg)
	if len(matches) > 1 {
		if secs, err := strconv.Atoi(matches[1]); err == nil {
			waitTime = time.Duration(secs+60) * time.Second
		}
	}

	return &RateLimitError{
		RetryAfter: time.Now().Add(waitTime),
		Message:    errMsg,
	}
}

type RateLimitError struct {
	RetryAfter time.Time
	Message    string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("RATE_LIMIT: %s", e.Message)
}
