package integration

import (
	"context"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
)

type SyncResult struct {
	DiscoveredCount int
	Error           error
}

type DecryptedTxInfo struct {
	Tx          domain.BankTransaction
	Amount      float64
	Receiver    string
	Description string
}

type Account struct {
	ID           string
	Name         string
	Balance      float64
	Enabled      bool
	IBAN         string
	BackoffUntil *time.Time
}

type TransactionMetadata struct {
	Amount       float64
	Receiver     string
	ReceiverIBAN string
	Description  string
	CreatedAt    time.Time
	ExternalID   string
}

type Provider interface {
	ServiceType() string
	Sync(ctx context.Context, integration *domain.Integration, force bool) SyncResult
	ParseTransaction(decryptedData []byte, accountID string) (TransactionMetadata, error)
	GetAccounts(userID string, integration *domain.Integration) ([]Account, error)
}
