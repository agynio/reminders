package store

import (
	"fmt"

	"github.com/google/uuid"
)

type NotFoundError struct {
	ReminderID uuid.UUID
}

func (e NotFoundError) Error() string {
	return fmt.Sprintf("reminder %s not found", e.ReminderID)
}

type InvalidStatusError struct {
	ReminderID uuid.UUID
	Status     ReminderStatus
}

func (e InvalidStatusError) Error() string {
	return fmt.Sprintf("reminder %s has status %s", e.ReminderID, e.Status)
}
