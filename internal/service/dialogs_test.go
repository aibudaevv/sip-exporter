package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewDialoger(t *testing.T) {
	d := NewDialoger()
	require.NotNil(t, d)
}

func TestDialoger_Create(t *testing.T) {
	d := NewDialoger()
	require.NotNil(t, d)

	expiresAt := time.Now().Add(1 * time.Hour)
	d.Create("dialog-1", expiresAt)

	require.Equal(t, 1, d.Size())
}

func TestDialoger_Create_ExistingDialog(t *testing.T) {
	d := NewDialoger()

	// Attempt to create existing dialog (should not overwrite)
	firstExpires := time.Now().Add(1 * time.Hour)
	d.Create("dialog-1", firstExpires)

	secondExpires := time.Now().Add(2 * time.Hour)
	d.Create("dialog-1", secondExpires)

	// Size should not change
	require.Equal(t, 1, d.Size())
}

func TestDialoger_Delete(t *testing.T) {
	d := NewDialoger()

	d.Create("dialog-1", time.Now().Add(1*time.Hour))
	require.Equal(t, 1, d.Size())

	d.Delete("dialog-1")
	require.Equal(t, 0, d.Size())
}

func TestDialoger_Delete_NonExisting(t *testing.T) {
	d := NewDialoger()

	// Deleting non-existing dialog should not panic
	d.Delete("non-existing")
	require.Equal(t, 0, d.Size())
}

func TestDialoger_Size_Multiple(t *testing.T) {
	d := NewDialoger()

	d.Create("dialog-1", time.Now().Add(1*time.Hour))
	d.Create("dialog-2", time.Now().Add(1*time.Hour))
	d.Create("dialog-3", time.Now().Add(1*time.Hour))

	require.Equal(t, 3, d.Size())

	d.Delete("dialog-2")
	require.Equal(t, 2, d.Size())
}

func TestDialoger_Cleanup_Expired(t *testing.T) {
	d := NewDialoger()

	expiredExpires := time.Now().Add(-1 * time.Hour)
	d.Create("expired-dialog", expiredExpires)

	validExpires := time.Now().Add(1 * time.Hour)
	d.Create("valid-dialog", validExpires)

	require.Equal(t, 2, d.Size())

	// Cleanup should remove only expired dialogs
	d.Cleanup()

	require.Equal(t, 1, d.Size())
}

func TestDialoger_Cleanup_AllExpired(t *testing.T) {
	d := NewDialoger()

	d.Create("expired-1", time.Now().Add(-1*time.Hour))
	d.Create("expired-2", time.Now().Add(-2*time.Hour))
	d.Create("expired-3", time.Now().Add(-3*time.Hour))

	require.Equal(t, 3, d.Size())

	// Cleanup should remove all dialogs
	d.Cleanup()

	require.Equal(t, 0, d.Size())
}

func TestDialoger_Cleanup_NoneExpired(t *testing.T) {
	d := NewDialoger()

	d.Create("valid-1", time.Now().Add(1*time.Hour))
	d.Create("valid-2", time.Now().Add(2*time.Hour))
	d.Create("valid-3", time.Now().Add(3*time.Hour))

	require.Equal(t, 3, d.Size())

	// Cleanup should not remove valid dialogs
	d.Cleanup()

	require.Equal(t, 3, d.Size())
}

func TestDialoger_Cleanup_Empty(t *testing.T) {
	d := NewDialoger()

	// Cleanup on empty storage
	d.Cleanup()

	require.Equal(t, 0, d.Size())
}

func TestDialoger_Concurrent_Create(t *testing.T) {
	d := NewDialoger()
	done := make(chan bool, 100)

	// Test thread safety for Create
	for i := 0; i < 100; i++ {
		go func(id int) {
			d.Create("dialog-"+string(rune(id)), time.Now().Add(1*time.Hour))
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	// All dialogs should be created
	require.Equal(t, 100, d.Size())
}

func TestDialoger_Concurrent_Delete(t *testing.T) {
	d := NewDialoger()

	// Create dialogs
	for i := 0; i < 50; i++ {
		d.Create("dialog-"+string(rune(i)), time.Now().Add(1*time.Hour))
	}

	done := make(chan bool, 50)

	// Test thread safety for Delete
	for i := 0; i < 50; i++ {
		go func(id int) {
			d.Delete("dialog-" + string(rune(id)))
			done <- true
		}(i)
	}

	for i := 0; i < 50; i++ {
		<-done
	}

	require.Equal(t, 0, d.Size())
}

func TestDialoger_Concurrent_Cleanup(t *testing.T) {
	d := NewDialoger()

	// Create dialogs with different expiration times
	for i := 0; i < 50; i++ {
		if i%2 == 0 {
			d.Create("expired-"+string(rune(i)), time.Now().Add(-1*time.Hour))
		} else {
			d.Create("valid-"+string(rune(i)), time.Now().Add(1*time.Hour))
		}
	}

	done := make(chan bool, 10)

	// Test thread safety for Cleanup
	for i := 0; i < 10; i++ {
		go func() {
			d.Cleanup()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Only valid dialogs should remain (25 total)
	require.Equal(t, 25, d.Size())
}
