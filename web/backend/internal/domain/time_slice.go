package domain

import "time"

type TimeSlice struct {
	ID             string
	Value          float64
	IntervalMonths int
	StartDate      time.Time
	EndDate        *time.Time
	Description    string
}
