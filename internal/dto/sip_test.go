package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPacket_Struct(t *testing.T) {
	// MC/DC: Проверка структуры Packet
	p := Packet{
		IsResponse:     true,
		ResponseStatus: []byte("200"),
		Method:         []byte("INVITE"),
		From: From{
			Addr: []byte("sip:user@domain"),
			Tag:  []byte("abc123"),
		},
		To: To{
			Addr: []byte("sip:other@domain"),
			Tag:  []byte("xyz789"),
		},
		CallID:         []byte("test-call-id"),
		CSeq:           CSeq{ID: []byte("1"), Method: []byte("INVITE")},
		SessionExpires: 1800,
	}

	require.True(t, p.IsResponse)
	require.Equal(t, []byte("200"), p.ResponseStatus)
	require.Equal(t, []byte("INVITE"), p.Method)
	require.Equal(t, []byte("sip:user@domain"), p.From.Addr)
	require.Equal(t, []byte("abc123"), p.From.Tag)
	require.Equal(t, []byte("sip:other@domain"), p.To.Addr)
	require.Equal(t, []byte("xyz789"), p.To.Tag)
	require.Equal(t, []byte("test-call-id"), p.CallID)
	require.Equal(t, []byte("1"), p.CSeq.ID)
	require.Equal(t, []byte("INVITE"), p.CSeq.Method)
	require.Equal(t, 1800, p.SessionExpires)
}

func TestPacket_Request(t *testing.T) {
	// MC/DC: Проверка структуры Packet для запроса
	p := Packet{
		IsResponse: false,
		Method:     []byte("REGISTER"),
		From: From{
			Tag: []byte("from-tag"),
		},
		To: To{
			Tag: []byte("to-tag"),
		},
		CallID: []byte("call-id"),
		CSeq:   CSeq{ID: []byte("100"), Method: []byte("REGISTER")},
	}

	require.False(t, p.IsResponse)
	require.Nil(t, p.ResponseStatus)
	require.Equal(t, []byte("REGISTER"), p.Method)
}

func TestPacket_WithSessionExpires(t *testing.T) {
	// MC/DC: Проверка SessionExpires поля
	p := Packet{
		IsResponse:     true,
		ResponseStatus: []byte("200"),
		CSeq:           CSeq{Method: []byte("INVITE")},
		SessionExpires: 3600,
	}

	require.Equal(t, 3600, p.SessionExpires)
}

func TestPacket_Empty(t *testing.T) {
	// MC/DC: Проверка пустой структуры
	p := Packet{}

	require.False(t, p.IsResponse)
	require.Nil(t, p.ResponseStatus)
	require.Nil(t, p.Method)
	require.Empty(t, p.From.Addr)
	require.Empty(t, p.From.Tag)
	require.Empty(t, p.To.Addr)
	require.Empty(t, p.To.Tag)
	require.Empty(t, p.CallID)
	require.Empty(t, p.CSeq.ID)
	require.Empty(t, p.CSeq.Method)
	require.Equal(t, 0, p.SessionExpires)
}

func TestFrom_Struct(t *testing.T) {
	// MC/DC: Проверка структуры From
	f := From{
		Addr: []byte("sip:100@domain"),
		Tag:  []byte("tag123"),
	}

	require.Equal(t, []byte("sip:100@domain"), f.Addr)
	require.Equal(t, []byte("tag123"), f.Tag)
}

func TestFrom_Empty(t *testing.T) {
	// MC/DC: Проверка пустой структуры From
	f := From{}

	require.Empty(t, f.Addr)
	require.Empty(t, f.Tag)
}

func TestTo_Struct(t *testing.T) {
	// MC/DC: Проверка структуры To
	to := To{
		Addr: []byte("sip:200@domain"),
		Tag:  []byte("to-tag-456"),
	}

	require.Equal(t, []byte("sip:200@domain"), to.Addr)
	require.Equal(t, []byte("to-tag-456"), to.Tag)
}

func TestTo_Empty(t *testing.T) {
	// MC/DC: Проверка пустой структуры To
	to := To{}

	require.Empty(t, to.Addr)
	require.Empty(t, to.Tag)
}

func TestCSeq_Struct(t *testing.T) {
	// MC/DC: Проверка структуры CSeq
	cseq := CSeq{
		ID:     []byte("12345"),
		Method: []byte("BYE"),
	}

	require.Equal(t, []byte("12345"), cseq.ID)
	require.Equal(t, []byte("BYE"), cseq.Method)
}

func TestCSeq_Empty(t *testing.T) {
	// MC/DC: Проверка пустой структуры CSeq
	cseq := CSeq{}

	require.Empty(t, cseq.ID)
	require.Empty(t, cseq.Method)
}

func TestPacket_Copy(t *testing.T) {
	// MC/DC: Проверка копирования структуры
	original := Packet{
		IsResponse:     true,
		ResponseStatus: []byte("200"),
		Method:         []byte("INVITE"),
		From:           From{Addr: []byte("sip:a@b"), Tag: []byte("tag1")},
		To:             To{Addr: []byte("sip:c@d"), Tag: []byte("tag2")},
		CallID:         []byte("call-id"),
		CSeq:           CSeq{ID: []byte("1"), Method: []byte("INVITE")},
		SessionExpires: 1800,
	}

	// Копируем структуру
	copy := original

	// Проверяем что значения скопированы
	require.Equal(t, original.IsResponse, copy.IsResponse)
	require.Equal(t, original.ResponseStatus, copy.ResponseStatus)
	require.Equal(t, original.Method, copy.Method)
	require.Equal(t, original.From, copy.From)
	require.Equal(t, original.To, copy.To)
	require.Equal(t, original.CallID, copy.CallID)
	require.Equal(t, original.CSeq, copy.CSeq)
	require.Equal(t, original.SessionExpires, copy.SessionExpires)
}

func TestPacket_ByteSliceModification(t *testing.T) {
	// MC/DC: Проверка что модификация byte slice влияет на структуру
	p := Packet{
		Method: []byte("INVITE"),
	}

	// Модифицируем slice
	p.Method = append(p.Method[:0], []byte("BYE")...)

	require.Equal(t, []byte("BYE"), p.Method)
}

// Тесты для проверки различных комбинаций полей
func TestPacket_Combinations(t *testing.T) {
	testCases := []struct {
		name     string
		packet   Packet
		validate func(t *testing.T, p Packet)
	}{
		{
			name: "INVITE request",
			packet: Packet{
				IsResponse: false,
				Method:     []byte("INVITE"),
				From:       From{Tag: []byte("from")},
				To:         To{},
				CallID:     []byte("id1"),
				CSeq:       CSeq{ID: []byte("1"), Method: []byte("INVITE")},
			},
			validate: func(t *testing.T, p Packet) {
				require.False(t, p.IsResponse)
				require.Equal(t, []byte("INVITE"), p.Method)
			},
		},
		{
			name: "200 OK response",
			packet: Packet{
				IsResponse:     true,
				ResponseStatus: []byte("200"),
				From:           From{Tag: []byte("from")},
				To:             To{Tag: []byte("to")},
				CallID:         []byte("id2"),
				CSeq:           CSeq{ID: []byte("1"), Method: []byte("INVITE")},
				SessionExpires: 1800,
			},
			validate: func(t *testing.T, p Packet) {
				require.True(t, p.IsResponse)
				require.Equal(t, []byte("200"), p.ResponseStatus)
				require.Equal(t, 1800, p.SessionExpires)
			},
		},
		{
			name: "BYE request",
			packet: Packet{
				IsResponse: false,
				Method:     []byte("BYE"),
				From:       From{Tag: []byte("from")},
				To:         To{Tag: []byte("to")},
				CallID:     []byte("id3"),
				CSeq:       CSeq{ID: []byte("2"), Method: []byte("BYE")},
			},
			validate: func(t *testing.T, p Packet) {
				require.False(t, p.IsResponse)
				require.Equal(t, []byte("BYE"), p.Method)
			},
		},
		{
			name: "404 response",
			packet: Packet{
				IsResponse:     true,
				ResponseStatus: []byte("404"),
				From:           From{Tag: []byte("from")},
				To:             To{Tag: []byte("to")},
				CallID:         []byte("id4"),
				CSeq:           CSeq{ID: []byte("3"), Method: []byte("REGISTER")},
			},
			validate: func(t *testing.T, p Packet) {
				require.True(t, p.IsResponse)
				require.Equal(t, []byte("404"), p.ResponseStatus)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.validate(t, tc.packet)
		})
	}
}
