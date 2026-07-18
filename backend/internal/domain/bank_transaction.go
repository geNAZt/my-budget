package domain

import "time"

type BankTransaction struct {
	ID                   string
	UserID               string
	IntegrationID        string
	AccountID            string
	SourceAccountID      string
	DestinationAccountID string
	PoolIDs              []string
	Tags                 string // Plaintext, comma-separated
	ExternalID           string
	EncryptedData        string
	LinkedTransactionID  *string
	IsLinkConfirmed      bool
	CorrelationID        string
	IsDeleted            bool
	DeniedDuplicateIDs   string
	InternalStatus       string    // e.g. PENDING_REJECTION, RECONCILED, EXPIRED_REJECTION
	CreatedAt            time.Time // Timestamp at source
	SyncedAt             time.Time // Timestamp at ingestion
}

type GenericTransaction struct {
	Amount         float64
	Description    string
	Peer           string
	PeerIBAN       string
	CreatedAt      time.Time
	ExternalID     string
	InternalStatus string
}
