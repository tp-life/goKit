package service

import "errors"

var (
	ErrExecutionPlanNotFound   = errors.New("execution plan not found")
	ErrExecutionActionInFlight = errors.New("execution action in flight")
)
