package service

import (
	"strconv"
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
	d.Create("dialog-1", expiresAt, time.Now(), "test-carrier")

	require.Equal(t, 1, d.Size())
}

func TestDialoger_Create_ExistingDialog(t *testing.T) {
	d := NewDialoger()

	firstExpires := time.Now().Add(1 * time.Hour)
	d.Create("dialog-1", firstExpires, time.Now(), "")

	secondExpires := time.Now().Add(2 * time.Hour)
	d.Create("dialog-1", secondExpires, time.Now(), "")

	require.Equal(t, 1, d.Size())
}

func TestDialoger_Delete(t *testing.T) {
	d := NewDialoger()

	createdAt := time.Now()
	d.Create("dialog-1", time.Now().Add(1*time.Hour), createdAt, "")
	require.Equal(t, 1, d.Size())

	duration := d.Delete("dialog-1")
	require.Equal(t, 0, d.Size())
	require.GreaterOrEqual(t, duration, time.Duration(0))
}

func TestDialoger_Delete_NonExisting(t *testing.T) {
	d := NewDialoger()

	duration := d.Delete("non-existing")
	require.Equal(t, 0, d.Size())
	require.Equal(t, time.Duration(0), duration)
}

func TestDialoger_Size_Multiple(t *testing.T) {
	d := NewDialoger()

	d.Create("dialog-1", time.Now().Add(1*time.Hour), time.Now(), "")
	d.Create("dialog-2", time.Now().Add(1*time.Hour), time.Now(), "")
	d.Create("dialog-3", time.Now().Add(1*time.Hour), time.Now(), "")

	require.Equal(t, 3, d.Size())

	d.Delete("dialog-2")
	require.Equal(t, 2, d.Size())
}

func TestDialoger_Cleanup_Expired(t *testing.T) {
	start := time.Now()
	d := NewDialoger()

	expiredExpires := time.Now().Add(-1 * time.Hour)
	d.Create("expired-dialog", expiredExpires, start.Add(-2*time.Hour), "")

	validExpires := time.Now().Add(1 * time.Hour)
	d.Create("valid-dialog", validExpires, time.Now(), "")

	require.Equal(t, 2, d.Size())

	durations := d.Cleanup()

	require.Equal(t, 1, d.Size())
	require.Len(t, durations, 1)
	require.Greater(t, durations[0], time.Hour)
	t.Logf("duration: %v", time.Since(start))
}

func TestDialoger_Cleanup_AllExpired(t *testing.T) {
	start := time.Now()
	d := NewDialoger()

	d.Create("expired-1", time.Now().Add(-1*time.Hour), start.Add(-3*time.Hour), "")
	d.Create("expired-2", time.Now().Add(-2*time.Hour), start.Add(-4*time.Hour), "")
	d.Create("expired-3", time.Now().Add(-3*time.Hour), start.Add(-5*time.Hour), "")

	require.Equal(t, 3, d.Size())

	durations := d.Cleanup()

	require.Equal(t, 0, d.Size())
	require.Len(t, durations, 3)
	t.Logf("duration: %v", time.Since(start))
}

func TestDialoger_Cleanup_NoneExpired(t *testing.T) {
	start := time.Now()
	d := NewDialoger()

	d.Create("valid-1", time.Now().Add(1*time.Hour), time.Now(), "")
	d.Create("valid-2", time.Now().Add(2*time.Hour), time.Now(), "")
	d.Create("valid-3", time.Now().Add(3*time.Hour), time.Now(), "")

	require.Equal(t, 3, d.Size())

	durations := d.Cleanup()

	require.Equal(t, 3, d.Size())
	require.Empty(t, durations)
	t.Logf("duration: %v", time.Since(start))
}

func TestDialoger_Cleanup_Empty(t *testing.T) {
	start := time.Now()
	d := NewDialoger()

	durations := d.Cleanup()

	require.Equal(t, 0, d.Size())
	require.Empty(t, durations)
	t.Logf("duration: %v", time.Since(start))
}

func TestDialoger_Delete_ReturnsDuration(t *testing.T) {
	d := NewDialoger()

	createdAt := time.Now().Add(-5 * time.Second)
	d.Create("dialog-1", time.Now().Add(1*time.Hour), createdAt, "")

	duration := d.Delete("dialog-1")
	require.GreaterOrEqual(t, duration, 5*time.Second)
}

func TestDialoger_Concurrent_Create(t *testing.T) {
	d := NewDialoger()
	done := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		go func(id int) {
			d.Create("dialog-"+strconv.Itoa(id), time.Now().Add(1*time.Hour), time.Now(), "")
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	require.Equal(t, 100, d.Size())
}

func TestDialoger_Concurrent_Delete(t *testing.T) {
	d := NewDialoger()

	for i := 0; i < 50; i++ {
		d.Create("dialog-"+strconv.Itoa(i), time.Now().Add(1*time.Hour), time.Now(), "")
	}

	done := make(chan bool, 50)

	for i := 0; i < 50; i++ {
		go func(id int) {
			d.Delete("dialog-" + strconv.Itoa(id))
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

	for i := 0; i < 50; i++ {
		if i%2 == 0 {
			d.Create("expired-"+strconv.Itoa(i), time.Now().Add(-1*time.Hour), time.Now(), "")
		} else {
			d.Create("valid-"+strconv.Itoa(i), time.Now().Add(1*time.Hour), time.Now(), "")
		}
	}

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			d.Cleanup()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	require.Equal(t, 25, d.Size())
}

func TestDialogs_SizeByCarrier(t *testing.T) {
	d := NewDialoger()
	d.Create("id1", time.Now().Add(time.Hour), time.Now(), "provider-a")
	d.Create("id2", time.Now().Add(time.Hour), time.Now(), "provider-a")
	d.Create("id3", time.Now().Add(time.Hour), time.Now(), "provider-b")
	byCarrier := d.SizeByCarrier()
	require.Equal(t, 2, byCarrier["provider-a"])
	require.Equal(t, 1, byCarrier["provider-b"])
}

func TestDialogs_SizeByCarrier_Empty(t *testing.T) {
	d := NewDialoger()
	byCarrier := d.SizeByCarrier()
	require.Empty(t, byCarrier)
}
