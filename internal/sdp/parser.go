// Package sdp parses SDP (RFC 4566) media descriptions from SIP message bodies
// for RTP stream correlation. Only audio media relevant to the IPv4-only eBPF
// capture path are returned.
package sdp

import (
	"bytes"
	"strconv"
	"strings"
)

const (
	mediaAudio  = "audio"
	ipVerIPv4   = "IP4"
	ipNullHold  = "0.0.0.0"
	connFields  = 3 // "c=IN <addrtype> <addr>"
	encMinParts = 2 // a=rtpmap encoding needs at least <enc>/<clock>
	encMaxParts = 3 // <enc>/<clock>/<params>
	ptEncParts  = 2 // "a=rtpmap: <pt> <encoding>"
)

// Media describes one audio media line of an SDP body, resolved for correlation.
type Media struct {
	IP         string           // connection IP (per-media c= || session c= || o= fallback)
	Port       uint16           // port from m=audio <port>
	Codecs     map[uint8]string // payload type → codec name (from a=rtpmap)
	ClockRates map[uint8]uint32 // payload type → clock rate Hz (from a=rtpmap)
}

// Parse parses an SDP body and returns the active audio media descriptions.
// Held (c=0.0.0.0), inactive (a=inactive) and IPv6 media are skipped — they do
// not produce RTP on the IPv4 capture path. When c= is absent entirely the
// origin (o=) IP is used as a best-effort fallback.
func Parse(body []byte) []Media {
	if len(body) == 0 {
		return nil
	}
	lines := bytes.Split(body, []byte("\n"))
	sessionIP := ""
	originIP := ""
	sessionCSeen := false
	var result []Media

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(string(lines[i]))
		switch {
		case strings.HasPrefix(line, "o="):
			originIP = extractOriginIP(line)
		case strings.HasPrefix(line, "m="):
			media, consumed, ok := parseMedia(lines, i, sessionIP)
			i += consumed
			if !ok {
				continue
			}
			if media.IP == "" {
				// best-effort fallback when no c= is present at all
				media.IP = originIP
			}
			// Skip hold (0.0.0.0) and non-IPv4: no RTP on the IPv4 capture path.
			if media.IP == ipNullHold || detectIPVer(media.IP) != ipVerIPv4 {
				continue
			}
			result = append(result, media)
		case strings.HasPrefix(line, "c=") && !sessionCSeen && !hasMediaBefore(lines, i):
			// session-level connection: the first c= appearing before any m=
			ip, ver := extractConnIP(line)
			if ver == ipVerIPv4 {
				sessionIP = ip
			}
			sessionCSeen = true
		}
	}

	return result
}

// parseMedia parses one m=audio section starting at lines[start] until the next
// m= line or end. Returns the resolved Media, the number of additional lines
// consumed (after the m= line), and ok=false if the section must be skipped.
func parseMedia(lines [][]byte, start int, sessionIP string) (Media, int, bool) {
	first := strings.TrimSpace(string(lines[start]))
	// "m=audio <port> ..."
	fields := strings.Fields(strings.TrimPrefix(first, "m="))
	if len(fields) < 2 || fields[0] != mediaAudio {
		// non-audio media: consume its subsection but produce nothing
		consumed := consumeUntilNextMedia(lines, start+1)
		return Media{}, consumed, false
	}

	port, err := strconv.Atoi(fields[1])
	if err != nil || port <= 0 || port > 65535 {
		consumed := consumeUntilNextMedia(lines, start+1)
		return Media{}, consumed, false
	}

	media := Media{
		IP:         sessionIP,
		Port:       uint16(port),
		Codecs:     make(map[uint8]string),
		ClockRates: make(map[uint8]uint32),
	}
	inactive := false
	mediaIP := ""

	j := start + 1
	for ; j < len(lines); j++ {
		line := strings.TrimSpace(string(lines[j]))
		switch {
		case strings.HasPrefix(line, "m="):
			j-- // leave this m= for the outer loop
			goto done
		case strings.HasPrefix(line, "c="):
			ip, _ := extractConnIP(line)
			mediaIP = ip
		case line == "a=inactive":
			inactive = true
		case strings.HasPrefix(line, "a=rtpmap:"):
			parseRtpmap(line, media.Codecs, media.ClockRates)
		}
	}
done:
	consumed := j - start

	if inactive {
		return Media{}, consumed, false
	}
	if mediaIP != "" {
		media.IP = mediaIP
	}
	// IP filtering (hold / IPv6 / empty→origin fallback) is applied by the caller.
	return media, consumed, true
}

func consumeUntilNextMedia(lines [][]byte, from int) int {
	for j := from; j < len(lines); j++ {
		if bytes.HasPrefix(bytes.TrimSpace(lines[j]), []byte("m=")) {
			return j - from + 1
		}
	}
	return len(lines) - from
}

// extractConnIP parses "c=IN IP4 <addr>" / "c=IN IP6 <addr>" and returns (addr, version).
func extractConnIP(line string) (string, string) {
	rest := strings.TrimPrefix(line, "c=")
	fields := strings.Fields(rest)
	if len(fields) < connFields {
		return "", ""
	}
	return fields[2], fields[1] // addr, version (IN <ver> <addr>)
}

// extractOriginIP parses "o=<user> <sess-id> <sess-ver> IN IP4 <addr>".
func extractOriginIP(line string) string {
	rest := strings.TrimPrefix(line, "o=")
	fields := strings.Fields(rest)
	// o= has 6 fields: username sess-id sess-version nettype addrtype addr
	if len(fields) >= 6 && fields[3] == "IN" {
		return fields[5]
	}
	return ""
}

func parseRtpmap(line string, codecs map[uint8]string, clocks map[uint8]uint32) {
	// a=rtpmap:<pt> <encoding>/<clock>[/<encparams>]
	rest := strings.TrimPrefix(line, "a=rtpmap:")
	ptEnc := strings.SplitN(rest, " ", ptEncParts)
	if len(ptEnc) != ptEncParts {
		return
	}
	pt, err := strconv.Atoi(strings.TrimSpace(ptEnc[0]))
	if err != nil || pt < 0 || pt > 127 {
		return
	}
	encParts := strings.SplitN(strings.TrimSpace(ptEnc[1]), "/", encMaxParts)
	if len(encParts) < encMinParts {
		return
	}
	codec := encParts[0]
	clock, err := strconv.ParseUint(encParts[1], 10, 32)
	if err != nil {
		return
	}
	codecs[uint8(pt)] = codec
	clocks[uint8(pt)] = uint32(clock)
}

// detectIPVer returns "IP4" for IPv4 literals and "IP6" otherwise.
func detectIPVer(ip string) string {
	if ip == "" {
		return ""
	}
	// IPv4 literals contain only digits and dots.
	for _, r := range ip {
		if (r < '0' || r > '9') && r != '.' {
			return "IP6"
		}
	}
	return ipVerIPv4
}

// hasMediaBefore reports whether any m= line precedes index i (i.e. this c=
// belongs to a media section, not the session level).
func hasMediaBefore(lines [][]byte, i int) bool {
	for k := range i {
		if bytes.HasPrefix(bytes.TrimSpace(lines[k]), []byte("m=")) {
			return true
		}
	}
	return false
}
