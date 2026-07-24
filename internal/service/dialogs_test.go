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
	d.Create(
		DialogParams{
			DialogID:  "dialog-1",
			ExpiresAt: expiresAt,
			CreatedAt: time.Now(),
			Carrier:   "test-carrier",
		},
	)

	require.Equal(t, 1, d.Size())
}

func TestDialoger_Create_ExistingDialog(t *testing.T) {
	d := NewDialoger()

	firstExpires := time.Now().Add(1 * time.Hour)
	d.Create(DialogParams{DialogID: "dialog-1", ExpiresAt: firstExpires, CreatedAt: time.Now()})

	secondExpires := time.Now().Add(2 * time.Hour)
	d.Create(DialogParams{DialogID: "dialog-1", ExpiresAt: secondExpires, CreatedAt: time.Now()})

	require.Equal(t, 1, d.Size())
}

func TestDialoger_Delete(t *testing.T) {
	d := NewDialoger()

	createdAt := time.Now()
	d.Create(
		DialogParams{
			DialogID:  "dialog-1",
			ExpiresAt: time.Now().Add(1 * time.Hour),
			CreatedAt: createdAt,
		},
	)
	require.Equal(t, 1, d.Size())

	result := d.Delete("dialog-1")
	require.Equal(t, 0, d.Size())
	require.GreaterOrEqual(t, result.Duration, time.Duration(0))
}

func TestDialoger_Delete_NonExisting(t *testing.T) {
	d := NewDialoger()

	result := d.Delete("non-existing")
	require.Equal(t, 0, d.Size())
	require.Equal(t, time.Duration(0), result.Duration)
}

func TestDialoger_HasActiveDialog(t *testing.T) {
	d := NewDialoger()
	d.Create(
		DialogParams{
			DialogID:  "dialog-1",
			ExpiresAt: time.Now().Add(1 * time.Hour),
			CreatedAt: time.Now(),
		},
	)

	require.True(t, d.HasActiveDialog("dialog-1"))
	require.False(t, d.HasActiveDialog("non-existing"))

	d.Delete("dialog-1")
	require.False(t, d.HasActiveDialog("dialog-1"))
}

func TestDialoger_Refresh(t *testing.T) {
	d := NewDialoger()
	createdAt := time.Now()
	originalExpires := time.Now().Add(1 * time.Hour)
	d.Create(DialogParams{
		DialogID: "dialog-1", ExpiresAt: originalExpires, CreatedAt: createdAt,
		Carrier: "carrier-a", UAType: "yealink", SourceCountry: "RU", CallID: "call-1",
	})

	newExpires := time.Now().Add(2 * time.Hour)
	ok := d.Refresh("dialog-1", newExpires)
	require.True(t, ok)

	require.False(t, d.Refresh("non-existing", newExpires))
}

func TestDialoger_Refresh_PreservesCreatedAt(t *testing.T) {
	d := NewDialoger()
	createdAt := time.Now()
	d.Create(DialogParams{
		DialogID: "dialog-1", ExpiresAt: time.Now().Add(1 * time.Hour), CreatedAt: createdAt,
		Carrier: "carrier-a", UAType: "yealink", SourceCountry: "RU", CallID: "call-1",
	})

	d.Refresh("dialog-1", time.Now().Add(2*time.Hour))
	result := d.Delete("dialog-1")

	require.GreaterOrEqual(t, result.Duration, time.Duration(0))
	require.Equal(t, "carrier-a", result.Carrier)
	require.Equal(t, "call-1", result.CallID)
}

func TestDialoger_Size_Multiple(t *testing.T) {
	d := NewDialoger()

	d.Create(
		DialogParams{
			DialogID:  "dialog-1",
			ExpiresAt: time.Now().Add(1 * time.Hour),
			CreatedAt: time.Now(),
		},
	)
	d.Create(
		DialogParams{
			DialogID:  "dialog-2",
			ExpiresAt: time.Now().Add(1 * time.Hour),
			CreatedAt: time.Now(),
		},
	)
	d.Create(
		DialogParams{
			DialogID:  "dialog-3",
			ExpiresAt: time.Now().Add(1 * time.Hour),
			CreatedAt: time.Now(),
		},
	)

	require.Equal(t, 3, d.Size())

	d.Delete("dialog-2")
	require.Equal(t, 2, d.Size())
}

func TestDialoger_Cleanup_Expired(t *testing.T) {
	start := time.Now()
	d := NewDialoger()

	expiredExpires := time.Now().Add(-1 * time.Hour)
	d.Create(
		DialogParams{
			DialogID:  "expired-dialog",
			ExpiresAt: expiredExpires,
			CreatedAt: start.Add(-2 * time.Hour),
		},
	)

	validExpires := time.Now().Add(1 * time.Hour)
	d.Create(DialogParams{DialogID: "valid-dialog", ExpiresAt: validExpires, CreatedAt: time.Now()})

	require.Equal(t, 2, d.Size())

	results := d.Cleanup()

	require.Equal(t, 1, d.Size())
	require.Len(t, results, 1)
	require.Greater(t, results[0].Duration, time.Hour)
	t.Logf("duration: %v", time.Since(start))
}

func TestDialoger_Cleanup_AllExpired(t *testing.T) {
	start := time.Now()
	d := NewDialoger()

	d.Create(DialogParams{
		DialogID: "expired-1", ExpiresAt: time.Now().Add(-1 * time.Hour), CreatedAt: start.Add(-3 * time.Hour),
	})
	d.Create(DialogParams{
		DialogID: "expired-2", ExpiresAt: time.Now().Add(-2 * time.Hour), CreatedAt: start.Add(-4 * time.Hour),
	})
	d.Create(DialogParams{
		DialogID: "expired-3", ExpiresAt: time.Now().Add(-3 * time.Hour), CreatedAt: start.Add(-5 * time.Hour),
	})

	require.Equal(t, 3, d.Size())

	results := d.Cleanup()

	require.Equal(t, 0, d.Size())
	require.Len(t, results, 3)
	t.Logf("duration: %v", time.Since(start))
}

func TestDialoger_Cleanup_NoneExpired(t *testing.T) {
	start := time.Now()
	d := NewDialoger()

	d.Create(
		DialogParams{
			DialogID:  "valid-1",
			ExpiresAt: time.Now().Add(1 * time.Hour),
			CreatedAt: time.Now(),
		},
	)
	d.Create(
		DialogParams{
			DialogID:  "valid-2",
			ExpiresAt: time.Now().Add(2 * time.Hour),
			CreatedAt: time.Now(),
		},
	)
	d.Create(
		DialogParams{
			DialogID:  "valid-3",
			ExpiresAt: time.Now().Add(3 * time.Hour),
			CreatedAt: time.Now(),
		},
	)

	require.Equal(t, 3, d.Size())

	results := d.Cleanup()

	require.Equal(t, 3, d.Size())
	require.Empty(t, results)
	t.Logf("duration: %v", time.Since(start))
}

func TestDialoger_Cleanup_Empty(t *testing.T) {
	start := time.Now()
	d := NewDialoger()

	results := d.Cleanup()

	require.Equal(t, 0, d.Size())
	require.Empty(t, results)
	t.Logf("duration: %v", time.Since(start))
}

func TestDialoger_Delete_ReturnsDuration(t *testing.T) {
	d := NewDialoger()

	createdAt := time.Now().Add(-5 * time.Second)
	d.Create(
		DialogParams{
			DialogID:  "dialog-1",
			ExpiresAt: time.Now().Add(1 * time.Hour),
			CreatedAt: createdAt,
		},
	)

	result := d.Delete("dialog-1")
	require.GreaterOrEqual(t, result.Duration, 5*time.Second)
}

func TestDialoger_Concurrent_Create(t *testing.T) {
	d := NewDialoger()
	done := make(chan bool, 100)

	for i := range 100 {
		go func(id int) {
			d.Create(
				DialogParams{
					DialogID:  "dialog-" + strconv.Itoa(id),
					ExpiresAt: time.Now().Add(1 * time.Hour),
					CreatedAt: time.Now(),
				},
			)
			done <- true
		}(i)
	}

	for range 100 {
		<-done
	}

	require.Equal(t, 100, d.Size())
}

func TestDialoger_Concurrent_Delete(t *testing.T) {
	d := NewDialoger()

	for i := range 50 {
		d.Create(
			DialogParams{
				DialogID:  "dialog-" + strconv.Itoa(i),
				ExpiresAt: time.Now().Add(1 * time.Hour),
				CreatedAt: time.Now(),
			},
		)
	}

	done := make(chan bool, 50)

	for i := range 50 {
		go func(id int) {
			d.Delete("dialog-" + strconv.Itoa(id))
			done <- true
		}(i)
	}

	for range 50 {
		<-done
	}

	require.Equal(t, 0, d.Size())
}

func TestDialoger_Concurrent_Cleanup(t *testing.T) {
	d := NewDialoger()

	for i := range 50 {
		if i%2 == 0 {
			d.Create(
				DialogParams{
					DialogID:  "expired-" + strconv.Itoa(i),
					ExpiresAt: time.Now().Add(-1 * time.Hour),
					CreatedAt: time.Now(),
				},
			)
		} else {
			d.Create(DialogParams{DialogID: "valid-" + strconv.Itoa(i), ExpiresAt: time.Now().Add(1 * time.Hour), CreatedAt: time.Now()})
		}
	}

	done := make(chan bool, 10)

	for range 10 {
		go func() {
			d.Cleanup()
			done <- true
		}()
	}

	for range 10 {
		<-done
	}

	require.Equal(t, 25, d.Size())
}

func TestDialogs_Counts(t *testing.T) {
	d := NewDialoger()
	d.Create(
		DialogParams{
			DialogID:  "id1",
			ExpiresAt: time.Now().Add(time.Hour),
			CreatedAt: time.Now(),
			Carrier:   "provider-a",
			UAType:    "yealink",
		},
	)
	d.Create(
		DialogParams{
			DialogID:  "id2",
			ExpiresAt: time.Now().Add(time.Hour),
			CreatedAt: time.Now(),
			Carrier:   "provider-a",
			UAType:    "yealink",
		},
	)
	d.Create(
		DialogParams{
			DialogID:  "id3",
			ExpiresAt: time.Now().Add(time.Hour),
			CreatedAt: time.Now(),
			Carrier:   "provider-b",
			UAType:    "grandstream",
		},
	)
	counts := d.Counts()
	require.Len(t, counts, 2)
	for _, lc := range counts {
		switch lc.Labels["carrier"] {
		case "provider-a":
			require.Equal(t, "yealink", lc.Labels["ua_type"])
			require.Equal(t, 2, lc.Count)
		case "provider-b":
			require.Equal(t, "grandstream", lc.Labels["ua_type"])
			require.Equal(t, 1, lc.Count)
		}
	}
}

func TestDialogs_Counts_Empty(t *testing.T) {
	d := NewDialoger()
	counts := d.Counts()
	require.Empty(t, counts)
}

func TestDialoger_Delete_ReturnsCarrier(t *testing.T) {
	d := NewDialoger()
	createdAt := time.Now()
	d.Create(
		DialogParams{
			DialogID:  "dialog-1",
			ExpiresAt: time.Now().Add(1 * time.Hour),
			CreatedAt: createdAt,
			Carrier:   "test-carrier",
		},
	)
	result := d.Delete("dialog-1")
	require.Equal(t, "test-carrier", result.Carrier)
	require.GreaterOrEqual(t, result.Duration, time.Duration(0))
}

func TestDialoger_Cleanup_ReturnsCarrier(t *testing.T) {
	d := NewDialoger()
	d.Create(
		DialogParams{
			DialogID:  "expired-carrier-a",
			ExpiresAt: time.Now().Add(-1 * time.Hour),
			CreatedAt: time.Now().Add(-2 * time.Hour),
			Carrier:   "carrier-a",
		},
	)
	d.Create(
		DialogParams{
			DialogID:  "expired-carrier-b",
			ExpiresAt: time.Now().Add(-1 * time.Hour),
			CreatedAt: time.Now().Add(-2 * time.Hour),
			Carrier:   "carrier-b",
		},
	)
	d.Create(
		DialogParams{
			DialogID:  "valid-dialog",
			ExpiresAt: time.Now().Add(1 * time.Hour),
			CreatedAt: time.Now(),
			Carrier:   "carrier-c",
		},
	)
	results := d.Cleanup()
	require.Len(t, results, 2)
	carriers := map[string]bool{results[0].Carrier: true, results[1].Carrier: true}
	require.True(t, carriers["carrier-a"])
	require.True(t, carriers["carrier-b"])
	require.Equal(t, 1, d.Size())
}

func TestDialoger_Delete_NonExisting_ReturnsEmptyCarrier(t *testing.T) {
	d := NewDialoger()
	result := d.Delete("non-existing")
	require.Equal(t, time.Duration(0), result.Duration)
	require.Empty(t, result.Carrier)
}

func TestDialoger_Delete_ReturnsUAType(t *testing.T) {
	d := NewDialoger()
	createdAt := time.Now()
	d.Create(
		DialogParams{
			DialogID:  "dialog-1",
			ExpiresAt: time.Now().Add(1 * time.Hour),
			CreatedAt: createdAt,
			Carrier:   "test-carrier",
			UAType:    "yealink",
		},
	)
	result := d.Delete("dialog-1")
	require.Equal(t, "test-carrier", result.Carrier)
	require.Equal(t, "yealink", result.UAType)
	require.GreaterOrEqual(t, result.Duration, time.Duration(0))
}

func TestDialoger_Cleanup_ReturnsUAType(t *testing.T) {
	d := NewDialoger()
	d.Create(
		DialogParams{
			DialogID:  "expired-1",
			ExpiresAt: time.Now().Add(-1 * time.Hour),
			CreatedAt: time.Now().Add(-2 * time.Hour),
			Carrier:   "carrier-a",
			UAType:    "yealink",
		},
	)
	d.Create(
		DialogParams{
			DialogID:  "expired-2",
			ExpiresAt: time.Now().Add(-1 * time.Hour),
			CreatedAt: time.Now().Add(-2 * time.Hour),
			Carrier:   "carrier-b",
			UAType:    "grandstream",
		},
	)
	results := d.Cleanup()
	require.Len(t, results, 2)
	uaTypes := map[string]bool{}
	for _, r := range results {
		uaTypes[r.UAType] = true
	}
	require.True(t, uaTypes["yealink"])
	require.True(t, uaTypes["grandstream"])
}

func TestDialoger_Delete_NonExisting_ReturnsEmptyUAType(t *testing.T) {
	d := NewDialoger()
	result := d.Delete("non-existing")
	require.Equal(t, time.Duration(0), result.Duration)
	require.Empty(t, result.Carrier)
	require.Empty(t, result.UAType)
}

func TestDialoger_Cleanup_ReturnsCallID(t *testing.T) {
	d := NewDialoger()
	d.Create(DialogParams{
		DialogID:  "dialog-1",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
		CreatedAt: time.Now().Add(-2 * time.Hour),
		Carrier:   "carrier-a",
		UAType:    "yealink",
		CallID:    "call-id-xyz",
	})

	results := d.Cleanup()
	require.Len(t, results, 1)
	require.Equal(
		t,
		"call-id-xyz",
		results[0].CallID,
		"expired dialog must return its Call-ID for media cleanup",
	)
}

func TestDialoger_Delete_ReturnsCallID(t *testing.T) {
	d := NewDialoger()
	d.Create(
		DialogParams{
			DialogID:  "dialog-1",
			ExpiresAt: time.Now().Add(1 * time.Hour),
			CreatedAt: time.Now(),
			Carrier:   "carrier-a",
			CallID:    "call-id-del",
		},
	)

	require.Equal(t, "call-id-del", d.Delete("dialog-1").CallID)
}

func TestDialoger_Delete_DestinationCountry(t *testing.T) {
	tests := []struct {
		name          string
		preCreate     bool
		createdOffset time.Duration
		destCountry   string
		deleteID      string
		wantCountry   string
		wantDuration  time.Duration
	}{
		{
			name:          "normal",
			preCreate:     true,
			createdOffset: 0,
			destCountry:   "US",
			deleteID:      "dialog-1",
			wantCountry:   "US",
		},
		{
			name:        "non_existing",
			preCreate:   false,
			deleteID:    "non-existing",
			wantCountry: "",
		},
		{
			name:          "clock_skew_future_created",
			preCreate:     true,
			createdOffset: 1 * time.Hour,
			destCountry:   "US",
			deleteID:      "dialog-1",
			wantCountry:   "US",
			wantDuration:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDialoger()
			if tt.preCreate {
				d.Create(DialogParams{
					DialogID: tt.deleteID, ExpiresAt: time.Now().Add(1 * time.Hour),
					CreatedAt: time.Now().Add(tt.createdOffset),
					Carrier:   "carrier-a", UAType: "yealink", SourceCountry: "RU",
					DestinationCountry: tt.destCountry, CallID: "call-1",
				})
			}
			result := d.Delete(tt.deleteID)
			require.Equal(t, tt.wantCountry, result.DestinationCountry)
			if tt.preCreate {
				require.GreaterOrEqual(t, result.Duration, tt.wantDuration)
			}
		})
	}
}

func TestDialoger_Cleanup_DestinationCountry(t *testing.T) {
	tests := []struct {
		name          string
		createdOffset time.Duration
		wantResults   int
		wantCountry   string
		wantRemaining int
	}{
		{
			name:          "normal_expired",
			createdOffset: -2 * time.Hour,
			wantResults:   1,
			wantCountry:   "US",
			wantRemaining: 0,
		},
		{
			name:          "silent_drop_non_positive_duration",
			createdOffset: 1 * time.Hour,
			wantResults:   0,
			wantRemaining: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDialoger()
			d.Create(DialogParams{
				DialogID: "expired-1", ExpiresAt: time.Now().Add(-1 * time.Hour),
				CreatedAt: time.Now().Add(tt.createdOffset),
				Carrier:   "carrier-a", UAType: "yealink", SourceCountry: "RU",
				DestinationCountry: "US", CallID: "call-1",
			})

			require.Equal(t, 1, d.Size())
			results := d.Cleanup()

			require.Len(t, results, tt.wantResults)
			require.Equal(t, tt.wantRemaining, d.Size())
			if tt.wantResults > 0 {
				require.Equal(t, tt.wantCountry, results[0].DestinationCountry)
			}
		})
	}
}
