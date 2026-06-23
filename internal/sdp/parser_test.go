package sdp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse_SessionLevelConnection(t *testing.T) {
	body := []byte("v=0\r\n" +
		"o=- 1 1 IN IP4 10.0.0.1\r\n" +
		"s=-\r\n" +
		"c=IN IP4 10.0.0.1\r\n" +
		"t=0 0\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n")
	media := Parse(body)
	require.Len(t, media, 1)
	require.Equal(t, "10.0.0.1", media[0].IP)
	require.Equal(t, uint16(5004), media[0].Port)
	require.Equal(t, "PCMU", media[0].Codecs[0])
	require.EqualValues(t, 8000, media[0].ClockRates[0])
}

func TestParse_PerMediaConnectionOverridesSession(t *testing.T) {
	body := []byte("c=IN IP4 10.0.0.1\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n" +
		"c=IN IP4 10.0.0.99\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n")
	media := Parse(body)
	require.Len(t, media, 1)
	require.Equal(t, "10.0.0.99", media[0].IP, "per-media c= must override session-level")
}

func TestParse_HoldConnectionSkipped(t *testing.T) {
	body := []byte("c=IN IP4 0.0.0.0\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n")
	media := Parse(body)
	require.Empty(t, media, "0.0.0.0 (hold) must not be registered")
}

func TestParse_OriginFallbackWhenNoConnection(t *testing.T) {
	body := []byte("o=- 1 1 IN IP4 10.0.0.7\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n")
	media := Parse(body)
	require.Len(t, media, 1)
	require.Equal(t, "10.0.0.7", media[0].IP, "must fall back to o= when c= is absent")
}

func TestParse_InactiveSkipped(t *testing.T) {
	body := []byte("c=IN IP4 10.0.0.1\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n" +
		"a=inactive\r\n")
	media := Parse(body)
	require.Empty(t, media, "a=inactive must not be registered")
}

func TestParse_SendOnlyRegistered(t *testing.T) {
	body := []byte("c=IN IP4 10.0.0.1\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n" +
		"a=sendonly\r\n")
	media := Parse(body)
	require.Len(t, media, 1, "sendonly (one-way audio) must still be registered")
}

func TestParse_VideoIgnored(t *testing.T) {
	body := []byte("c=IN IP4 10.0.0.1\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n" +
		"m=video 5006 RTP/AVP 31\r\n")
	media := Parse(body)
	require.Len(t, media, 1, "only audio media must be parsed")
	require.Equal(t, uint16(5004), media[0].Port)
}

func TestParse_VideoThenAudio(t *testing.T) {
	// video-first SDP (common for video-capable UAs): the audio section after
	// the video section must still be parsed (regression: off-by-one used to skip it).
	body := []byte("c=IN IP4 10.0.0.1\r\n" +
		"m=video 5006 RTP/AVP 31\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n")
	media := Parse(body)
	require.Len(t, media, 1, "audio section after video must be parsed")
	require.Equal(t, uint16(5004), media[0].Port)
}

func TestParse_MultipleAudioMedia(t *testing.T) {
	body := []byte("c=IN IP4 10.0.0.1\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n" +
		"m=audio 5006 RTP/AVP 8\r\n" +
		"a=rtpmap:8 PCMA/8000\r\n")
	media := Parse(body)
	require.Len(t, media, 2)
	require.Equal(t, uint16(5004), media[0].Port)
	require.Equal(t, uint16(5006), media[1].Port)
}

func TestParse_DynamicPayloadFromRtpmap(t *testing.T) {
	body := []byte("c=IN IP4 10.0.0.1\r\n" +
		"m=audio 5004 RTP/AVP 111\r\n" +
		"a=rtpmap:111 opus/48000/2\r\n")
	media := Parse(body)
	require.Len(t, media, 1)
	require.Equal(t, "opus", media[0].Codecs[111])
	require.EqualValues(t, 48000, media[0].ClockRates[111])
}

func TestParse_IPv6Skipped(t *testing.T) {
	body := []byte("c=IN IP6 ::1\r\n" +
		"m=audio 5004 RTP/AVP 0\r\n")
	media := Parse(body)
	require.Empty(t, media, "IPv6 media is not captured by IPv4-only eBPF")
}

func TestParse_EmptyBody(t *testing.T) {
	require.Empty(t, Parse(nil))
	require.Empty(t, Parse([]byte("")))
}
