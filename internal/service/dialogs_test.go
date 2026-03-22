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

	// MC/DC: Создаем новый диалог
	expiresAt := time.Now().Add(1 * time.Hour)
	d.Create("dialog-1", expiresAt)

	require.Equal(t, 1, d.Size())
}

func TestDialoger_Create_ExistingDialog(t *testing.T) {
	d := NewDialoger()

	// MC/DC: Попытка создать существующий диалог (не должен перезаписываться)
	firstExpires := time.Now().Add(1 * time.Hour)
	d.Create("dialog-1", firstExpires)

	secondExpires := time.Now().Add(2 * time.Hour)
	d.Create("dialog-1", secondExpires)

	// Размер не должен измениться
	require.Equal(t, 1, d.Size())
}

func TestDialoger_Delete(t *testing.T) {
	d := NewDialoger()

	// Создаем диалог
	d.Create("dialog-1", time.Now().Add(1*time.Hour))
	require.Equal(t, 1, d.Size())

	// MC/DC: Удаляем существующий диалог
	d.Delete("dialog-1")
	require.Equal(t, 0, d.Size())
}

func TestDialoger_Delete_NonExisting(t *testing.T) {
	d := NewDialoger()

	// MC/DC: Удаление несуществующего диалога (не должно вызывать панику)
	d.Delete("non-existing")
	require.Equal(t, 0, d.Size())
}

func TestDialoger_Size_Multiple(t *testing.T) {
	d := NewDialoger()

	// Создаем несколько диалогов
	d.Create("dialog-1", time.Now().Add(1*time.Hour))
	d.Create("dialog-2", time.Now().Add(1*time.Hour))
	d.Create("dialog-3", time.Now().Add(1*time.Hour))

	require.Equal(t, 3, d.Size())

	// Удаляем один
	d.Delete("dialog-2")
	require.Equal(t, 2, d.Size())
}

func TestDialoger_Cleanup_Expired(t *testing.T) {
	d := NewDialoger()

	// Создаем диалог с истекшим сроком
	expiredExpires := time.Now().Add(-1 * time.Hour)
	d.Create("expired-dialog", expiredExpires)

	// Создаем диалог с будущим сроком
	validExpires := time.Now().Add(1 * time.Hour)
	d.Create("valid-dialog", validExpires)

	require.Equal(t, 2, d.Size())

	// MC/DC: Cleanup должен удалить только истекшие диалоги
	d.Cleanup()

	require.Equal(t, 1, d.Size())
}

func TestDialoger_Cleanup_AllExpired(t *testing.T) {
	d := NewDialoger()

	// Создаем несколько истекших диалогов
	d.Create("expired-1", time.Now().Add(-1*time.Hour))
	d.Create("expired-2", time.Now().Add(-2*time.Hour))
	d.Create("expired-3", time.Now().Add(-3*time.Hour))

	require.Equal(t, 3, d.Size())

	// MC/DC: Cleanup должен удалить все диалоги
	d.Cleanup()

	require.Equal(t, 0, d.Size())
}

func TestDialoger_Cleanup_NoneExpired(t *testing.T) {
	d := NewDialoger()

	// Создаем несколько диалогов с будущим сроком
	d.Create("valid-1", time.Now().Add(1*time.Hour))
	d.Create("valid-2", time.Now().Add(2*time.Hour))
	d.Create("valid-3", time.Now().Add(3*time.Hour))

	require.Equal(t, 3, d.Size())

	// MC/DC: Cleanup не должен удалять валидные диалоги
	d.Cleanup()

	require.Equal(t, 3, d.Size())
}

func TestDialoger_Cleanup_Empty(t *testing.T) {
	d := NewDialoger()

	// MC/DC: Cleanup на пустом хранилище
	d.Cleanup()

	require.Equal(t, 0, d.Size())
}

func TestDialoger_Concurrent_Create(t *testing.T) {
	d := NewDialoger()
	done := make(chan bool, 100)

	// MC/DC: Проверка потокобезопасности Create
	for i := 0; i < 100; i++ {
		go func(id int) {
			d.Create("dialog-"+string(rune(id)), time.Now().Add(1*time.Hour))
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	// Все диалоги должны быть созданы
	require.Equal(t, 100, d.Size())
}

func TestDialoger_Concurrent_Delete(t *testing.T) {
	d := NewDialoger()

	// Создаем диалоги
	for i := 0; i < 50; i++ {
		d.Create("dialog-"+string(rune(i)), time.Now().Add(1*time.Hour))
	}

	done := make(chan bool, 50)

	// MC/DC: Проверка потокобезопасности Delete
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

	// Создаем диалоги с разными сроками
	for i := 0; i < 50; i++ {
		if i%2 == 0 {
			d.Create("expired-"+string(rune(i)), time.Now().Add(-1*time.Hour))
		} else {
			d.Create("valid-"+string(rune(i)), time.Now().Add(1*time.Hour))
		}
	}

	done := make(chan bool, 10)

	// MC/DC: Проверка потокобезопасности Cleanup
	for i := 0; i < 10; i++ {
		go func() {
			d.Cleanup()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Должны остаться только валидные диалоги (25 штук)
	require.Equal(t, 25, d.Size())
}
