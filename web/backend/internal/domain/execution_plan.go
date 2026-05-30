package domain

import "time"

type ExecutionPlan struct {
	ID           string
	UserID       string
	Name         string
	Code         string
	TriggerType  string // 'CRON', 'TRANSACTION'
	TriggerValue string // cron expression or event source ID
	IsEnabled    bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ExecutionLog struct {
	ID         string
	UserID     string
	PlanID     string
	Status     string // 'SUCCESS', 'FAILED', 'RUNNING'
	Stdout     string
	Stderr     string
	ExitCode   int
	StartedAt  time.Time
	FinishedAt *time.Time
}
