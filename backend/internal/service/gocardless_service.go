package service

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
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
		return "", resp.HTTPResponse, s.parseRateLimitError(resp.Body, resp.HTTPResponse)
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
		return nil, resp.HTTPResponse, s.parseRateLimitError(resp.Body, resp.HTTPResponse)
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
		return nil, s.parseRateLimitError(resp.Body, resp.HTTPResponse)
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
		return nil, s.parseRateLimitError(resp.Body, resp.HTTPResponse)
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
		return nil, s.parseRateLimitError(resp.Body, resp.HTTPResponse)
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
		return nil, s.parseRateLimitError(resp.Body, resp.HTTPResponse)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch account details (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	return resp.JSON200, nil
}

func (s *GoCardlessService) GetBalances(ctx context.Context, accountID string, token string) (*gocardless.AccountBalance, *http.Response, error) {
	client, err := s.getClient(ctx)
	if err != nil {
		return nil, nil, err
	}

	resp, err := client.RetrieveAccountBalancesWithResponse(ctx, accountID, func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode() == http.StatusTooManyRequests {
		return nil, resp.HTTPResponse, s.parseRateLimitError(resp.Body, resp.HTTPResponse)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, resp.HTTPResponse, fmt.Errorf("failed to fetch balances (Status %d): %s", resp.StatusCode(), string(resp.Body))
	}

	return resp.JSON200, resp.HTTPResponse, nil
}

func (s *GoCardlessService) ExtractRateLimit(resp *http.Response) *time.Time {
	if resp == nil {
		return nil
	}

	remaining := getHeader(resp.Header, "RateLimit-Remaining", "X-RateLimit-Remaining", "HTTP_X_RATELIMIT_REMAINING", "x-ratelimit-remaining")
	reset := getHeader(resp.Header, "RateLimit-Reset", "X-RateLimit-Reset", "HTTP_X_RATELIMIT_RESET", "x-ratelimit-reset", "Retry-After")

	if reset == "" {
		return nil
	}

	rem := -1
	if remaining != "" {
		rem, _ = strconv.Atoi(remaining)
	}

	// GoCardless is strict: back off if rate limited (429) or remaining is 0 or 1
	if resp.StatusCode == http.StatusTooManyRequests || (rem >= 0 && rem <= 1) {
		t := ParseResetTime(reset)
		if t != nil && t.After(time.Now()) {
			return t
		}
	}

	return nil
}

func (s *GoCardlessService) parseRateLimitError(body []byte, resp *http.Response) error {
	errMsg := string(body)

	var retryAfter *time.Time
	if resp != nil {
		reset := getHeader(resp.Header, "RateLimit-Reset", "X-RateLimit-Reset", "HTTP_X_RATELIMIT_RESET", "x-ratelimit-reset", "Retry-After")
		if reset != "" {
			retryAfter = ParseResetTime(reset)
		}
	}

	if retryAfter == nil {
		waitTime := 24 * time.Hour
		re := regexp.MustCompile(`in (\d+) seconds`)
		matches := re.FindStringSubmatch(errMsg)
		if len(matches) > 1 {
			if secs, err := strconv.Atoi(matches[1]); err == nil {
				waitTime = time.Duration(secs+60) * time.Second
			}
		}
		t := time.Now().Add(waitTime)
		retryAfter = &t
	}

	return &RateLimitError{
		RetryAfter: *retryAfter,
		Message:    errMsg,
	}
}

func ParseResetTime(headerVal string) *time.Time {
	headerVal = strings.TrimSpace(headerVal)
	if headerVal == "" {
		return nil
	}

	// 1. Try parsing as integer
	if res, err := strconv.ParseInt(headerVal, 10, 64); err == nil {
		if res <= 0 {
			return nil
		}
		// Milliseconds Unix epoch (> 100 billion, e.g. 1727193600000)
		if res > 100_000_000_000 {
			t := time.UnixMilli(res)
			return &t
		}
		// Seconds Unix epoch (> 1 billion, e.g. 1727193600)
		if res > 1_000_000_000 {
			t := time.Unix(res, 0)
			return &t
		}
		// Relative duration in seconds (< 1 billion, e.g. 5, 60, 3600, 86400)
		t := time.Now().Add(time.Duration(res) * time.Second)
		return &t
	}

	// 2. Try parsing as HTTP Date (RFC1123 / RFC850 / ANSIC)
	if t, err := http.ParseTime(headerVal); err == nil {
		return &t
	}

	// 3. Try parsing as RFC3339
	if t, err := time.Parse(time.RFC3339, headerVal); err == nil {
		return &t
	}

	return nil
}

func getHeader(h http.Header, keys ...string) string {
	if h == nil {
		return ""
	}
	for _, k := range keys {
		if v := h.Get(k); v != "" {
			return v
		}
	}
	for k, v := range h {
		kl := strings.ToLower(k)
		for _, target := range keys {
			if kl == strings.ToLower(target) && len(v) > 0 && v[0] != "" {
				return v[0]
			}
		}
	}
	return ""
}

type RateLimitError struct {
	RetryAfter time.Time
	Message    string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("RATE_LIMIT: %s", e.Message)
}
