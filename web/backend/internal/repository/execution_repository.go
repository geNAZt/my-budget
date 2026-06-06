package repository

import (
	"database/sql"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/google/uuid"
)

type ExecutionRepository struct {
	db *sql.DB
}

func NewExecutionRepository(db *sql.DB) *ExecutionRepository {
	return &ExecutionRepository{db: db}
}

func (r *ExecutionRepository) List(userID string) ([]domain.ExecutionPlan, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, name, code, trigger_type, trigger_value, is_enabled, created_at, updated_at
		FROM execution_plans
		WHERE user_id = ?
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []domain.ExecutionPlan
	for rows.Next() {
		var p domain.ExecutionPlan
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.Code, &p.TriggerType, &p.TriggerValue, &p.IsEnabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	if plans == nil {
		plans = []domain.ExecutionPlan{}
	}
	return plans, nil
}

func (r *ExecutionRepository) GetByID(userID string, id string) (*domain.ExecutionPlan, error) {
	var p domain.ExecutionPlan
	err := r.db.QueryRow(`
		SELECT id, user_id, name, code, trigger_type, trigger_value, is_enabled, created_at, updated_at
		FROM execution_plans
		WHERE user_id = ? AND id = ?`, userID, id).
		Scan(&p.ID, &p.UserID, &p.Name, &p.Code, &p.TriggerType, &p.TriggerValue, &p.IsEnabled, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *ExecutionRepository) Save(userID string, p *domain.ExecutionPlan) error {
	p.UpdatedAt = time.Now()

	var exists bool
	if p.ID != "" {
		err := r.db.QueryRow("SELECT EXISTS(SELECT 1 FROM execution_plans WHERE user_id = ? AND id = ?)", userID, p.ID).Scan(&exists)
		if err != nil {
			return err
		}
	}

	if !exists {
		if p.ID == "" {
			p.ID = uuid.New().String()
		}
		p.CreatedAt = time.Now()
		_, err := r.db.Exec(`
			INSERT INTO execution_plans (id, user_id, name, code, trigger_type, trigger_value, is_enabled, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, userID, p.Name, p.Code, p.TriggerType, p.TriggerValue, p.IsEnabled, p.CreatedAt, p.UpdatedAt)
		return err
	}

	_, err := r.db.Exec(`
		UPDATE execution_plans
		SET name = ?, code = ?, trigger_type = ?, trigger_value = ?, is_enabled = ?, updated_at = ?
		WHERE user_id = ? AND id = ?`,
		p.Name, p.Code, p.TriggerType, p.TriggerValue, p.IsEnabled, p.UpdatedAt, userID, p.ID)
	return err
}

func (r *ExecutionRepository) Delete(userID string, id string) error {
	_, err := r.db.Exec("DELETE FROM execution_plans WHERE user_id = ? AND id = ?", userID, id)
	return err
}

func (r *ExecutionRepository) LogExecution(log *domain.ExecutionLog) error {
	if log.ID == "" {
		log.ID = uuid.New().String()
	}
	_, err := r.db.Exec(`
		INSERT INTO execution_logs (id, user_id, plan_id, status, stdout, stderr, exit_code, started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.UserID, log.PlanID, log.Status, log.Stdout, log.Stderr, log.ExitCode, log.StartedAt, log.FinishedAt)
	return err
}

func (r *ExecutionRepository) GetLogsForPlan(userID string, planID string) ([]domain.ExecutionLog, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, plan_id, status, stdout, stderr, exit_code, started_at, finished_at
		FROM execution_logs
		WHERE user_id = ? AND plan_id = ?
		ORDER BY started_at DESC
		LIMIT 50`, userID, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []domain.ExecutionLog
	for rows.Next() {
		var l domain.ExecutionLog
		var finishedAt sql.NullTime
		if err := rows.Scan(&l.ID, &l.UserID, &l.PlanID, &l.Status, &l.Stdout, &l.Stderr, &l.ExitCode, &l.StartedAt, &finishedAt); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			l.FinishedAt = &finishedAt.Time
		}
		logs = append(logs, l)
	}
	if logs == nil {
		logs = []domain.ExecutionLog{}
	}
	return logs, nil
}
