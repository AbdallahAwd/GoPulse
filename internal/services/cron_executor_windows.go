//go:build windows

package services

import (
	"context"
	"fmt"
	"github.com/amaa/gopulse/internal/models"
)

func executeLocalCron(context.Context, models.CronJob, *cronOutput) (*int, bool, error) {
	return nil, false, fmt.Errorf("local cron commands require a Unix host; remote Linux servers are supported")
}
