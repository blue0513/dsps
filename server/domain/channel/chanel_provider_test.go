package channel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/saiya/dsps/server/config"
	"github.com/saiya/dsps/server/domain"
	"github.com/saiya/dsps/server/telemetry"
	dspstesting "github.com/saiya/dsps/server/testing"
)

func TestProvider(t *testing.T) {
	cfg, err := config.ParseConfig(context.Background(), config.Overrides{}, `channels: [ { regex: "test.+", expire: "1s" } ]`)
	assert.NoError(t, err)
	clock := dspstesting.NewStubClock(t)
	cp, err := NewChannelProvider(context.Background(), &cfg, clock, telemetry.NewEmptyTelemetry(t))
	assert.NoError(t, err)

	test1, err := cp.Get("test1")
	assert.NoError(t, err)
	test1Again, err := cp.Get("test1")
	assert.NoError(t, err)
	assert.NotNil(t, test1)
	assert.Same(t, test1, test1Again)

	notfound, err := cp.Get("not-found")
	assert.Nil(t, notfound)
	dspstesting.IsError(t, domain.ErrInvalidChannel, err)
}
