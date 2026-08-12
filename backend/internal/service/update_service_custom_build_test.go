package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateServiceCustomBuildDisablesSelfUpdate(t *testing.T) {
	svc := NewUpdateService(nil, nil, "0.1.175", "custom")

	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, "0.1.175", info.CurrentVersion)
	require.Equal(t, "0.1.175", info.LatestVersion)
	require.False(t, info.HasUpdate)
	require.Equal(t, "custom", info.BuildType)

	require.ErrorIs(t, svc.PerformUpdate(context.Background()), ErrCustomBuildUpdateDisabled)
}
