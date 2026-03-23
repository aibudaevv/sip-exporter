package constant

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConstants_Invite(t *testing.T) {
	// MC/DC: Проверка константы INVITE
	require.Equal(t, "INVITE", Invite)
	require.NotEmpty(t, Invite)
}

func TestConstants_Bye(t *testing.T) {
	// MC/DC: Проверка константы BYE
	require.Equal(t, "BYE", Bye)
	require.NotEmpty(t, Bye)
}

func TestConstants_StatusOK(t *testing.T) {
	// MC/DC: Проверка константы StatusOK
	require.Equal(t, "200", StatusOK)
	require.NotEmpty(t, StatusOK)
}

func TestConstants_Distinct(t *testing.T) {
	// MC/DC: Проверка что константы различны
	require.NotEqual(t, Invite, Bye)
	require.NotEqual(t, Invite, StatusOK)
	require.NotEqual(t, Bye, StatusOK)
}

func TestConstants_Length(t *testing.T) {
	// MC/DC: Проверка длины констант
	require.Greater(t, len(Invite), 0)
	require.Greater(t, len(Bye), 0)
	require.Greater(t, len(StatusOK), 0)
}
