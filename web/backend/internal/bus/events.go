package bus

import "github.com/genazt/my-budget-script/web/backend/internal/domain"

const (
	// TopicTransactionDiscovered is published when a new transaction is discovered during sync
	// Payload: TransactionDiscoveredPayload
	TopicTransactionDiscovered = "transaction.discovered"

	// TopicSyncFinished is published when an integration sync finishes
	// Payload: SyncFinishedPayload
	TopicSyncFinished = "sync.finished"

	// TopicRulesChanged is published when rules or pools are modified
	// Payload: userID (string)
	TopicRulesChanged = "rules.changed"

	// TopicIntegrationDeleted is published when an integration is deleted
	// Payload: IntegrationDeletedPayload
	TopicIntegrationDeleted = "integration.deleted"
)

type SyncFinishedPayload struct {
	UserID          string
	IntegrationID   string
	IntegrationName string
	ServiceType     string
	DiscoveredCount int
}

type TransactionDiscoveredPayload struct {
	UserID      string
	Tx          domain.BankTransaction
	Amount      float64
	Receiver    string
	Description string
}

type IntegrationDeletedPayload struct {
	UserID        string
	IntegrationID string
}
