package bootstrap

import (
	"context"
	"log/slog"
)

// RunWorker establishes the process lifecycle. No queue or executor is wired yet.
func RunWorker(ctx context.Context, logger *slog.Logger) {
	logger.Info("worker started", "mode", "idle", "task_processing_enabled", false)
	<-ctx.Done()
	logger.Info("worker stopped gracefully")
}
