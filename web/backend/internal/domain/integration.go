package domain

import "time"

type Integration struct {
	ID                  string
	UserID              string
	ServiceType         string // 'GOCARDLESS'
	Name                string
	EncryptedConfig     string
	Status              string // 'ACTIVE', 'ERROR', 'AWAITING_AUTH', 'RATE_LIMITED'
	SyncIntervalSeconds int
	LastSyncAt          *time.Time
	LastError           string
	CachedBalance       float64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type AccountMeta struct {
	Alias             string     `json:"Alias"`
	Enabled           bool       `json:"Enabled"`
	IBAN              string     `json:"IBAN"`
	BIC               string     `json:"BIC"`
	ReferenceCodes    string     `json:"ReferenceCodes"`
	Tags              string     `json:"Tags"`
	BackoffUntil      *time.Time `json:"BackoffUntil"`
	LastSyncedAt      *time.Time `json:"LastSyncedAt"`
	MetadataCheckedAt *time.Time `json:"MetadataCheckedAt"`
	Balance           float64    `json:"Balance"`
}
