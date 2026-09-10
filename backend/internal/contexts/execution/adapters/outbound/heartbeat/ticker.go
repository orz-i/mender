package heartbeat

import (
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
)

type Factory struct{}

func New() Factory { return Factory{} }

type ticker struct{ value *time.Ticker }

func (t *ticker) C() <-chan time.Time { return t.value.C }
func (t *ticker) Stop()               { t.value.Stop() }

func (Factory) NewTicker(interval time.Duration) (application.HeartbeatTicker, error) {
	if interval <= 0 {
		return nil, errors.New("invalid heartbeat interval")
	}
	return &ticker{value: time.NewTicker(interval)}, nil
}

var _ application.HeartbeatTickerFactory = Factory{}
