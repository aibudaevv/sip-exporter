package exporter

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"

	"github.com/aibudaevv/sip-exporter/internal/carriers"
	"github.com/aibudaevv/sip-exporter/internal/dto"
	"github.com/aibudaevv/sip-exporter/internal/mediatracker"
	"github.com/aibudaevv/sip-exporter/internal/rtp"
	"github.com/aibudaevv/sip-exporter/internal/sdp"
	"github.com/aibudaevv/sip-exporter/internal/service"
	"github.com/aibudaevv/sip-exporter/internal/ua"
	"github.com/aibudaevv/sip-exporter/internal/vq"
)

var (
	ErrUserNotRoot = errors.New("this program requires root privileges")
)

const (
	ethPAll            = 0x0003
	readBufSize        = 65536
	defaultRegisterTTL = 60 * time.Second
	defaultInviteTTL   = 60 * time.Second
	defaultOptionsTTL  = 60 * time.Second
	rtpStreamTTL       = 30 * time.Second // idle RTP stream expiry

	messagesChanSize = 10_000
	socketRecvBufMB  = 4
	socketRcvTimeo   = 1 * time.Second

	ethHeaderLen     = 14
	vlanEthTypeHi    = 0x81
	vlanEthTypeLo    = 0x00
	vlanHeaderLen    = 18
	ethTypeIPv4Hi    = 0x08
	ethTypeIPv4Lo    = 0x00
	ipV4MinHeaderLen = 20
	ipV4HdrLenMask   = 0x0F
	ipV4HdrLenShift  = 4
	ipProtoUDP       = 17
	udpHeaderLen     = 8
	minSIPDataLen    = 50
	minRawPacketLen  = ethHeaderLen + ipV4MinHeaderLen + udpHeaderLen
	minVLANPacketLen = vlanHeaderLen + ipV4MinHeaderLen + udpHeaderLen

	parseErrTypeL2  = "l2"
	parseErrTypeL3  = "l3"
	parseErrTypeL4  = "l4"
	parseErrTypeSIP = "sip"

	rtpVersionMask    = 0xC0
	rtpVersion2Prefix = 0x80

	sipPartsCount          = 3
	minSIPParts            = 2
	tagPrefixLen           = 5
	nanosPerMs     float64 = 1e6
	htonsShift             = 8
	htonsMask      uint16  = 0xFF00
	miB                    = 1024 * 1024
	defaultUAType          = "other"
	defaultCarrier         = "other"
)

type (
	registerEntry struct {
		timestamp time.Time
		carrier   string
		uaType    string
	}

	inviteEntry struct {
		timestamp   time.Time
		carrier     string
		uaType      string
		ttrMeasured bool
		pddMeasured bool
	}

	optionsEntry struct {
		timestamp time.Time
		carrier   string
		uaType    string
	}

	inviteSDPEntity struct {
		body      []byte
		timestamp time.Time
	}

	exporter struct {
		collection      *ebpf.Collection
		sock            int
		messages        chan []byte
		done            chan struct{}
		wg              sync.WaitGroup
		closeOnce       sync.Once
		sipPort         uint16
		sipsPort        uint16
		services        services
		carrierResolver *carriers.Resolver
		uaClassifier    *ua.Classifier
		vqHandler       *vq.Handler
		mediaTracker    *mediatracker.Tracker
		registerTracker map[string]registerEntry
		registerMutex   sync.RWMutex
		inviteTracker   map[string]inviteEntry
		inviteMutex     sync.RWMutex
		inviteSDP       map[string]inviteSDPEntity
		inviteSDPMutex  sync.Mutex
		optionsTracker  map[string]optionsEntry
		optionsMutex    sync.RWMutex
		initialized     atomic.Bool
	}
	services struct {
		metricser service.Metricser
		dialoger  service.Dialoger
	}
	Exporter interface {
		Initialize(
			interfaceName string,
			path string,
			sipPort, sipsPort int,
			ignoreOutgoing, rtpCapture bool,
			rtpStreamTTL time.Duration,
		) error
		IsAlive() bool
		Close()
	}
)

func NewExporter(
	m service.Metricser,
	d service.Dialoger,
	resolver *carriers.Resolver,
	classifier *ua.Classifier,
) Exporter {
	return &exporter{
		services: services{
			metricser: m,
			dialoger:  d,
		},
		carrierResolver: resolver,
		uaClassifier:    classifier,
		vqHandler:       vq.NewHandler(m),
		mediaTracker:    mediatracker.NewTracker(rtpStreamTTL),
		messages:        make(chan []byte, messagesChanSize),
		done:            make(chan struct{}),
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		inviteSDP:       make(map[string]inviteSDPEntity),
		optionsTracker:  make(map[string]optionsEntry),
	}
}

func (e *exporter) Initialize(
	interfaceName string, path string,
	sipPort, sipsPort int,
	ignoreOutgoing, rtpCapture bool,
	rtpStreamTTL time.Duration,
) error {
	if syscall.Geteuid() != 0 {
		return ErrUserNotRoot
	}

	// Apply the config-driven RTP stream expiry (RFC 3550 §6.3.5 idle-timeout).
	e.mediaTracker.SetTTL(rtpStreamTTL)

	collection, err := ebpf.LoadCollection(path)
	if err != nil {
		return fmt.Errorf("failed to load BPF collection: %w", err)
	}

	e.collection, e.sipPort, e.sipsPort = collection, uint16(sipPort), uint16(sipsPort)

	prog := collection.Programs["bpf_socket_filter"]
	if prog == nil {
		return errors.New("failed to find BPF program: bpf_socket_filter")
	}

	// Configure eBPF maps (SIP ports + RTP capture flag)
	if err = e.configureEBPFMaps(collection, sipPort, sipsPort, rtpCapture); err != nil {
		return err
	}

	// Create AF_PACKET socket with SOCK_RAW
	sock, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(ethPAll)))
	if err != nil {
		return fmt.Errorf("failed to create AF_PACKET socket: %w", err)
	}
	e.sock = sock

	socketRecvBufSize := socketRecvBufMB * miB
	if setErr := unix.SetsockoptInt(sock, unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, socketRecvBufSize); setErr != nil {
		if setErrFallback := unix.SetsockoptInt(sock, unix.SOL_SOCKET, unix.SO_RCVBUF, socketRecvBufSize); setErrFallback != nil {
			return fmt.Errorf("failed to set SO_RCVBUF: %w", setErrFallback)
		}
		zap.L().Warn("SO_RCVBUFFORCE failed, using SO_RCVBUF (buffer capped by rmem_max)", zap.Error(setErr))
	}

	var actualBufSize int
	actualBufSize, err = unix.GetsockoptInt(sock, unix.SOL_SOCKET, unix.SO_RCVBUF)
	if err != nil {
		return fmt.Errorf("failed to get SO_RCVBUF: %w", err)
	}
	zap.L().Info("socket receive buffer configured",
		zap.Int("requested_bytes", socketRecvBufSize),
		zap.Int("actual_bytes", actualBufSize))

	if setErr := unix.SetsockoptTimeval(sock, unix.SOL_SOCKET, unix.SO_RCVTIMEO,
		&unix.Timeval{Sec: int64(socketRcvTimeo / time.Second), Usec: int64(socketRcvTimeo % time.Second / time.Microsecond)}); setErr != nil {
		return fmt.Errorf("failed to set SO_RCVTIMEO: %w", setErr)
	}

	ifaceName := interfaceName
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("interface %s not found: %w", ifaceName, err)
	}

	sa := &unix.SockaddrLinklayer{
		Protocol: htons(ethPAll),
		Ifindex:  iface.Index,
	}

	err = unix.Bind(sock, sa)
	if err != nil {
		return fmt.Errorf("failed to bind AF_PACKET socket to %s: %w", ifaceName, err)
	}

	if ignoreOutgoing {
		if setErr := unix.SetsockoptInt(sock, unix.SOL_PACKET, unix.PACKET_IGNORE_OUTGOING, 1); setErr != nil {
			return fmt.Errorf("failed to set PACKET_IGNORE_OUTGOING: %w", setErr)
		}
		zap.L().Info("PACKET_IGNORE_OUTGOING enabled", zap.String("interface", ifaceName))
	}

	// Attach eBPF filter
	progFD := prog.FD()
	if err = unix.SetsockoptInt(sock, unix.SOL_SOCKET, unix.SO_ATTACH_BPF, progFD); err != nil {
		return fmt.Errorf("failed to attach BPF program: %w", err)
	}

	zap.L().Info("eBPF program attached to AF_PACKET socket",
		zap.String("interface", interfaceName),
		zap.Int("sip_port", sipPort),
		zap.Int("sips_port", sipsPort))

	go e.readPackets()
	e.wg.Add(1)
	go e.readSocket()
	go e.sipDialogMetricsUpdate()

	e.initialized.Store(true)

	return nil
}

// configureEBPFMaps populates the eBPF SIP-ports map and the RTP-capture flag map.
func (e *exporter) configureEBPFMaps(collection *ebpf.Collection, sipPort, sipsPort int, rtpCapture bool) error {
	sipPortsMap := collection.Maps["sip_ports"]
	if sipPortsMap == nil {
		return errors.New("failed to find sip_ports map")
	}
	if err := sipPortsMap.Update(uint32(0), uint16(sipPort), ebpf.UpdateAny); err != nil {
		return fmt.Errorf("failed to set SIP port: %w", err)
	}
	if err := sipPortsMap.Update(uint32(1), uint16(sipsPort), ebpf.UpdateAny); err != nil {
		return fmt.Errorf("failed to set SIPS port: %w", err)
	}

	rtpConfigMap := collection.Maps["rtp_config"]
	if rtpConfigMap == nil {
		return errors.New("failed to find rtp_config map")
	}
	rtpValue := uint8(0)
	if rtpCapture {
		rtpValue = 1
	}
	if err := rtpConfigMap.Update(uint32(0), rtpValue, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("failed to set RTP capture config: %w", err)
	}
	zap.L().Info("RTP capture configured", zap.Bool("enabled", rtpCapture))
	return nil
}

func extractIPs(ipHeader []byte) (net.IP, net.IP) {
	srcIP := net.IPv4(ipHeader[12], ipHeader[13], ipHeader[14], ipHeader[15])
	dstIP := net.IPv4(ipHeader[16], ipHeader[17], ipHeader[18], ipHeader[19])
	return srcIP, dstIP
}

func (e *exporter) resolveCarrier(ipHeader []byte) string {
	if e.carrierResolver == nil {
		return defaultCarrier
	}
	srcIP, dstIP := extractIPs(ipHeader)
	carrier := e.carrierResolver.Lookup(srcIP)
	if carrier == defaultCarrier {
		carrier = e.carrierResolver.Lookup(dstIP)
	}
	return carrier
}

func (e *exporter) resolveUA(userAgent []byte) string {
	return e.uaClassifier.Classify(userAgent)
}

func (e *exporter) sipDialogMetricsUpdate() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	e.services.metricser.UpdateChannelCapacity(cap(e.messages))

	for {
		select {
		case <-e.done:
			return
		case <-ticker.C:
		}
		results := e.services.dialoger.Cleanup()
		e.cleanupRegisterTracker()
		e.cleanupInviteTracker()
		e.cleanupInviteSDP()
		e.cleanupOptionsTracker()
		e.mediaTracker.Cleanup()
		s := e.services.dialoger.Size()

		for _, r := range results {
			e.services.metricser.SessionCompleted(r.Carrier, r.UAType)
			e.services.metricser.UpdateSPD(r.Carrier, r.UAType, r.Duration)
			e.mediaTracker.Unregister(r.CallID)
		}

		zap.L().Debug("update metrics", zap.Int("size dialogs", s), zap.Int("expired", len(results)))

		e.services.metricser.UpdateSessions(e.services.dialoger.Counts())

		received, dropped := e.readSocketStats()
		e.services.metricser.SocketStats(received, dropped)
		e.services.metricser.UpdateChannelLength(len(e.messages))
		e.services.metricser.UpdateTrackerSize("register", len(e.registerTracker))
		e.services.metricser.UpdateTrackerSize("invite", len(e.inviteTracker))
		e.services.metricser.UpdateTrackerSize("options", len(e.optionsTracker))
		e.services.metricser.UpdateTrackerSize("rtp", e.mediaTracker.StreamCount())
		e.updateRTPMetrics()
		e.services.metricser.UpdateActiveDialogs(s)
	}
}

func (e *exporter) cleanupRegisterTracker() {
	e.registerMutex.Lock()
	defer e.registerMutex.Unlock()

	now := time.Now()
	for callID, entry := range e.registerTracker {
		if now.Sub(entry.timestamp) > defaultRegisterTTL {
			delete(e.registerTracker, callID)
			zap.L().Debug("register tracker expired",
				zap.String("call_id", callID),
				zap.Duration("age", now.Sub(entry.timestamp)))
		}
	}
}

func (e *exporter) cleanupInviteTracker() {
	e.inviteMutex.Lock()
	defer e.inviteMutex.Unlock()

	now := time.Now()
	for callID, entry := range e.inviteTracker {
		if now.Sub(entry.timestamp) > defaultInviteTTL {
			delete(e.inviteTracker, callID)
			zap.L().Debug("invite tracker expired",
				zap.String("call_id", callID),
				zap.Duration("age", now.Sub(entry.timestamp)))
		}
	}
}

func (e *exporter) Close() {
	e.closeOnce.Do(func() {
		e.initialized.Store(false)
		close(e.done)
		if e.collection != nil {
			e.collection.Close()
		}
		if e.sock != 0 {
			_ = unix.Close(e.sock)
		}
		e.wg.Wait()
		close(e.messages)
	})
}

func (e *exporter) IsAlive() bool {
	return e.initialized.Load()
}

func (e *exporter) readSocketStats() (uint32, uint32) {
	if e.sock == 0 {
		return 0, 0
	}

	stats, err := unix.GetsockoptTpacketStats(e.sock, unix.SOL_PACKET, unix.PACKET_STATISTICS)
	if err != nil {
		zap.L().Debug("failed to read AF_PACKET stats", zap.Error(err))
		return 0, 0
	}
	return stats.Packets, stats.Drops
}

func (e *exporter) readPackets() {
	for packet := range e.messages {
		if errType, err := e.parseRawPacket(packet); err != nil {
			e.services.metricser.SystemError()
			e.services.metricser.ParseError(errType)
			zap.L().Error("parse err", zap.Error(err))
		}
	}
}

func (e *exporter) readSocket() {
	defer e.wg.Done()
	buf := make([]byte, readBufSize)

	for {
		n, err := unix.Read(e.sock, buf)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			if errors.Is(err, unix.EBADF) || errors.Is(err, unix.ENOTSOCK) {
				zap.L().Info("socket closed, shutting down readSocket")
				return
			}
			if err != unix.EAGAIN {
				zap.L().Error("socket read error", zap.Error(err))
				e.services.metricser.SystemError()
			}
			select {
			case <-e.done:
				return
			default:
				continue
			}
		}

		if n == 0 {
			continue
		}

		packet := make([]byte, n)
		copy(packet, buf[:n])

		zap.L().Debug("packet from socket", zap.Int("len", n))

		if !e.sendPacket(packet) {
			return
		}
	}
}

// sendPacket routes a packet to the messages channel. SIP packets (port
// 5060/5061) use a blocking send — they must not be starved by RTP flood.
// All other packets (RTP) use a non-blocking send — dropped when the channel
// is full. Returns false if shutdown was signaled.
func (e *exporter) sendPacket(packet []byte) bool {
	if e.isSIPPacket(packet) {
		select {
		case e.messages <- packet:
		case <-e.done:
			return false
		}
		return true
	}
	select {
	case e.messages <- packet:
	case <-e.done:
		return false
	default:
	}
	return true
}

// isSIPPacket does a quick L4 port check to classify a packet as SIP (port
// 5060/5061) or RTP/other. SIP packets use a blocking channel send (must not
// be starved by RTP flood); all other packets use a non-blocking send (dropped
// if the channel is full). When headers can't be parsed, defaults to true
// (blocking) to avoid dropping potentially critical traffic.
func (e *exporter) isSIPPacket(packet []byte) bool {
	if len(packet) < minRawPacketLen {
		return true
	}
	offset := ethHeaderLen
	if packet[12] == vlanEthTypeHi && packet[13] == vlanEthTypeLo {
		offset = vlanHeaderLen
	}
	ihl := int(packet[offset]&ipV4HdrLenMask) * ipV4HdrLenShift
	udpOff := offset + ihl
	if len(packet) < udpOff+udpHeaderLen {
		return true
	}
	srcPort := binary.BigEndian.Uint16(packet[udpOff : udpOff+2])
	dstPort := binary.BigEndian.Uint16(packet[udpOff+2 : udpOff+4])
	return srcPort == e.sipPort || srcPort == e.sipsPort ||
		dstPort == e.sipPort || dstPort == e.sipsPort
}

// parseRawPacket parses raw L2 packet. Returns error type (l2, l3, l4, sip) and error.
func (e *exporter) parseRawPacket(packet []byte) (string, error) {
	if len(packet) < minRawPacketLen {
		return parseErrTypeL2, fmt.Errorf("wrong len packet %d", len(packet))
	}

	// Parse Ethernet header (14 bytes)
	ethType := packet[12:14]
	ipOffset := ethHeaderLen

	// VLAN (802.1Q)
	if ethType[0] == vlanEthTypeHi && ethType[1] == vlanEthTypeLo {
		if len(packet) < minVLANPacketLen {
			return parseErrTypeL2, fmt.Errorf("wrong len packet %d", len(packet))
		}
		ethType = packet[16:18]
		ipOffset = vlanHeaderLen
	}

	// Only IPv4
	if ethType[0] != ethTypeIPv4Hi || ethType[1] != ethTypeIPv4Lo {
		return parseErrTypeL3, errors.New("not IPv4 packet")
	}

	// IP header
	if len(packet) < ipOffset+ipV4MinHeaderLen {
		return parseErrTypeL3, errors.New("ip header too short")
	}

	ipHeader := packet[ipOffset : ipOffset+ipV4MinHeaderLen]
	ihl := ipHeader[0] & ipV4HdrLenMask
	ipHeaderLen := int(ihl) * ipV4HdrLenShift
	carrier := e.resolveCarrier(ipHeader)

	if ipHeader[9] != ipProtoUDP {
		return parseErrTypeL4, errors.New("not UDP packet")
	}

	// UDP header (8 bytes)
	udpOffset := ipOffset + ipHeaderLen
	if len(packet) < udpOffset+udpHeaderLen {
		return parseErrTypeL4, errors.New("udp header too short")
	}

	// SIP data after UDP header
	sipOffset := udpOffset + udpHeaderLen
	if sipOffset >= len(packet) {
		return parseErrTypeSIP, errors.New("no SIP payload")
	}

	sipData := packet[sipOffset:]

	// RTP packets (passed by the eBPF filter with version=2) arrive truncated to
	// the RTP header. SIP messages start with an ASCII letter and never with the
	// 0x80-0xBF range, so the first payload byte unambiguously distinguishes RTP.
	if sipData[0]&rtpVersionMask == rtpVersion2Prefix {
		srcPort := binary.BigEndian.Uint16(packet[udpOffset : udpOffset+2])
		dstPort := binary.BigEndian.Uint16(packet[udpOffset+2 : udpOffset+4])
		srcIP, dstIP := extractIPs(ipHeader)
		return e.handleRTP(srcIP, srcPort, dstIP, dstPort, sipData)
	}

	// Minimum SIP packet should be at least 50 bytes
	if len(sipData) < minSIPDataLen {
		return parseErrTypeSIP, fmt.Errorf("packet too small for SIP: %d", len(sipData))
	}

	// Check if this is a SIP packet (starts with SIP method or SIP/2.0)
	if !bytes.HasPrefix(sipData, []byte("INVITE")) &&
		!bytes.HasPrefix(sipData, []byte("ACK")) &&
		!bytes.HasPrefix(sipData, []byte("BYE")) &&
		!bytes.HasPrefix(sipData, []byte("CANCEL")) &&
		!bytes.HasPrefix(sipData, []byte("OPTIONS")) &&
		!bytes.HasPrefix(sipData, []byte("REGISTER")) &&
		!bytes.HasPrefix(sipData, []byte("SUBSCRIBE")) &&
		!bytes.HasPrefix(sipData, []byte("NOTIFY")) &&
		!bytes.HasPrefix(sipData, []byte("PUBLISH")) &&
		!bytes.HasPrefix(sipData, []byte("INFO")) &&
		!bytes.HasPrefix(sipData, []byte("PRACK")) &&
		!bytes.HasPrefix(sipData, []byte("UPDATE")) &&
		!bytes.HasPrefix(sipData, []byte("MESSAGE")) &&
		!bytes.HasPrefix(sipData, []byte("REFER")) &&
		!bytes.HasPrefix(sipData, []byte("SIP/2.0")) {
		return parseErrTypeSIP, errors.New("not a SIP packet")
	}

	zap.L().Debug("packet raw", zap.ByteString("sip_data", sipData))

	if err := e.handleMessage(carrier, sipData); err != nil {
		return "sip", err
	}

	return "", nil
}

func (e *exporter) sipPacketParse(raw []byte) (dto.Packet, error) {
	lines := bytes.Split(raw, []byte("\r\n"))
	if len(lines) == 0 {
		return dto.Packet{}, fmt.Errorf("split return empty result, raw: %q", raw)
	}

	p := dto.Packet{}
	if bytes.HasPrefix(lines[0], []byte("SIP/2.0")) {
		p.IsResponse = true
		p.ResponseStatus = bytes.TrimPrefix(lines[0], []byte("SIP/2.0 "))[:3]
	} else {
		parts := bytes.SplitN(lines[0], []byte(" "), sipPartsCount)
		if len(parts) >= minSIPParts {
			p.IsResponse = false
			p.Method = bytes.TrimSpace(parts[0])
		}
	}

	if err := e.parseHeaders(lines, &p); err != nil {
		return dto.Packet{}, err
	}

	if idx := bytes.Index(raw, []byte("\r\n\r\n")); idx != -1 {
		p.Body = raw[idx+4:]
	}

	return p, nil
}

func (e *exporter) parseHeaders(lines [][]byte, p *dto.Packet) error {
	for i, line := range lines {
		if i == 0 {
			continue
		}

		header, value := splitHeader(line)

		switch {
		case bytes.Equal(header, []byte("From")):
			tag := extractTag(value)
			if tag == nil {
				return fmt.Errorf("fail extract tag from '%b'", value)
			}

			p.From.Tag = tag
			p.From.User, p.From.Addr = ParseURI(value)
		case bytes.Equal(header, []byte("To")):
			p.To.Tag = extractTag(value)
			p.To.User, p.To.Addr = ParseURI(value)
		case bytes.Equal(header, []byte("Call-ID")):
			p.CallID = value
		case bytes.Equal(header, []byte("CSeq")):
			id, method := extractCSeq(value)
			if id == nil || method == nil {
				return fmt.Errorf("fail extract CSeq from '%s'", value)
			}

			p.CSeq.Method = method
			p.CSeq.ID = id
		case bytes.Equal(header, []byte("Session-Expires")):
			p.SessionExpires = extractSessionExpires(value)
		case bytes.Equal(header, []byte("User-Agent")):
			if p.UserAgent == nil {
				p.UserAgent = value
			}
		case bytes.Equal(header, []byte("Content-Type")):
			if p.ContentType == nil {
				p.ContentType = value
			}
		}
	}

	return nil
}

func (e *exporter) handleMessage(carrier string, rawPacket []byte) error {
	packet, err := e.sipPacketParse(rawPacket)
	if err != nil {
		return fmt.Errorf("parse SIP packet: %w", err)
	}

	zap.L().Debug("parsed packet", zap.Any("packet", packet))

	uaType := e.resolveUA(packet.UserAgent)

	if packet.IsResponse {
		e.handleResponse(carrier, uaType, packet)
	} else {
		e.handleRequest(carrier, uaType, packet)
	}

	return nil
}

// handleRTP parses an RTP header and feeds it to the media tracker. Packets with
// no correlated SIP dialog are dropped silently (per design: RTP without an
// established dialog is not monitored). RTP stats are accumulated in the tracker
// and exported periodically (see sipDialogMetricsUpdate).
func (e *exporter) handleRTP(
	srcIP net.IP, srcPort uint16,
	dstIP net.IP, dstPort uint16,
	payload []byte,
) (string, error) {
	header, err := rtp.ParseHeader(payload)
	if err != nil {
		zap.L().Debug("RTP header parse skipped", zap.Error(err))
		return "", nil
	}
	res, ok := e.mediaTracker.Observe(srcIP.String(), srcPort, dstIP.String(), dstPort, header, time.Now())
	if !ok {
		// No correlated media endpoint for this flow → drop.
		return "", nil
	}
	if res.Counted {
		e.services.metricser.UpdateRTPPackets(res.Carrier, res.UAType, res.Codec)
	}
	if res.Lost > 0 {
		e.services.metricser.UpdateRTPLoss(res.Carrier, res.UAType, res.Codec, res.Lost)
	}
	return "", nil
}

// updateRTPMetrics emits the periodic RTP sample metrics (jitter, MOS histograms
// and the active-streams gauge) from the media tracker snapshot.
func (e *exporter) updateRTPMetrics() {
	stats := e.mediaTracker.Snapshot()
	if len(stats) == 0 {
		e.services.metricser.UpdateRTPActiveStreams(nil)
		return
	}
	type aggKey struct{ carrier, uaType, codec string }
	tmp := make(map[aggKey]int)
	for _, s := range stats {
		e.services.metricser.UpdateRTPJitter(s.Carrier, s.UAType, s.Codec, s.JitterMs)
		e.services.metricser.UpdateRTPMOS(s.Carrier, s.UAType, s.Codec, s.MOS)
		tmp[aggKey{s.Carrier, s.UAType, s.Codec}]++
	}
	counts := make([]service.LabeledCount, 0, len(tmp))
	for k, n := range tmp {
		counts = append(counts, service.LabeledCount{
			Labels: map[string]string{"carrier": k.carrier, "ua_type": k.uaType, "codec": k.codec},
			Count:  n,
		})
	}
	e.services.metricser.UpdateRTPActiveStreams(counts)
}

func (e *exporter) handleRequest(carrier string, uaType string, packet dto.Packet) {
	e.services.metricser.Request(carrier, uaType, packet.Method)

	if bytes.Equal(packet.Method, []byte("REGISTER")) {
		e.storeRegisterTime(string(packet.CallID), carrier, uaType)
	}

	if bytes.Equal(packet.Method, []byte("INVITE")) {
		e.storeInviteTime(string(packet.CallID), carrier, uaType)
		if bytes.Contains(packet.ContentType, []byte("application/sdp")) {
			e.storeInviteSDP(string(packet.CallID), packet.Body)
		}
	}

	if bytes.Equal(packet.Method, []byte("CANCEL")) {
		e.removeInviteTime(string(packet.CallID))
	}

	if bytes.Equal(packet.Method, []byte("OPTIONS")) {
		e.storeOptionsTime(string(packet.CallID), carrier, uaType)
	}

	if bytes.Contains(packet.ContentType, []byte("application/vq-rtcpxr")) {
		e.vqHandler.HandleVQReport(packet.Body, carrier, uaType)
	}
}

func (e *exporter) handleResponse(packetCarrier string, packetUAType string, packet dto.Packet) {
	isInviteResponse := bytes.Equal(packet.CSeq.Method, []byte("INVITE"))
	isRegisterResponse := bytes.Equal(packet.CSeq.Method, []byte("REGISTER"))
	is200OK := bytes.Equal(packet.ResponseStatus, []byte("200"))

	carrier := packetCarrier
	uaType := packetUAType

	if isInviteResponse {
		carrier, uaType = e.handleInviteResponse(carrier, uaType, packet)
	}

	if isRegisterResponse {
		if regCarrier, regUAType, ok := e.getRegisterCarrier(string(packet.CallID)); ok {
			carrier = regCarrier
			uaType = regUAType
		}
	}

	isOptionsResponse := bytes.Equal(packet.CSeq.Method, []byte("OPTIONS"))
	if isOptionsResponse {
		if delayMs, optionsCarrier, optionsUAType, ok := e.measureOptionsTime(string(packet.CallID)); ok {
			e.services.metricser.UpdateORD(optionsCarrier, optionsUAType, delayMs)
		}
	}

	e.services.metricser.ResponseWithMetrics(
		carrier,
		uaType,
		packet.ResponseStatus,
		isInviteResponse,
		is200OK,
	)

	if is200OK {
		e.handle200OKResponse(carrier, uaType, packet, isRegisterResponse)
	} else if isRegisterResponse {
		e.handleRegisterNon200Response(carrier, uaType, packet)
	}
}

func (e *exporter) handleInviteResponse(
	fallbackCarrier string,
	fallbackUAType string,
	packet dto.Packet,
) (string, string) {
	carrier := fallbackCarrier
	uaType := fallbackUAType
	if inviteCarrier, inviteUAType, ok := e.getInviteCarrier(string(packet.CallID)); ok {
		carrier = inviteCarrier
		uaType = inviteUAType
	}
	if len(packet.ResponseStatus) > 0 {
		if packet.ResponseStatus[0] == '1' {
			e.handleProvisionalResponse(packet)
		} else {
			e.removeInviteTime(string(packet.CallID))
		}
	}
	return carrier, uaType
}

func (e *exporter) handleProvisionalResponse(packet dto.Packet) {
	delayMs, inviteCarrier, inviteUAType, ok := e.readInviteEntry(string(packet.CallID))
	if !ok {
		return
	}
	callID := string(packet.CallID)
	if !e.isTTRMeasured(callID) {
		e.services.metricser.UpdateTTR(inviteCarrier, inviteUAType, delayMs)
		e.markTTRMeasured(callID)
	}
	if !e.isPDDMeasured(callID) {
		e.measurePDD(inviteCarrier, inviteUAType, delayMs, packet.ResponseStatus, callID)
	}
}

func (e *exporter) measurePDD(carrier string, uaType string, delayMs float64, status []byte, callID string) {
	if len(status) >= 3 && status[1] == '8' && status[2] == '0' {
		e.services.metricser.UpdatePDD(carrier, uaType, delayMs)
		e.markPDDMeasured(callID)
	}
}

func (e *exporter) handleRegisterNon200Response(carrier string, uaType string, packet dto.Packet) {
	if len(packet.ResponseStatus) > 0 && packet.ResponseStatus[0] == '3' {
		if startTime, ok := e.getRegisterTime(string(packet.CallID)); ok {
			delayMs := float64(time.Since(startTime).Nanoseconds()) / nanosPerMs
			e.services.metricser.UpdateLRD(carrier, uaType, delayMs)
			e.removeRegisterTime(string(packet.CallID))
			zap.L().Debug("LRD measured",
				zap.String("call_id", string(packet.CallID)),
				zap.Float64("delay_ms", delayMs))
		}
		return
	}
	e.removeRegisterTime(string(packet.CallID))
	zap.L().Debug("register tracker removed (non-200 non-3xx response)",
		zap.String("call_id", string(packet.CallID)),
		zap.ByteString("status", packet.ResponseStatus))
}

func (e *exporter) handle200OKResponse(carrier string, uaType string, packet dto.Packet, isRegisterResponse bool) {
	zap.L().Debug("handle message", zap.ByteString("200 OK cseq method", packet.CSeq.Method))

	if bytes.Equal(packet.CSeq.Method, []byte("INVITE")) {
		if err := e.handleInvite200OK(carrier, uaType, packet); err != nil {
			zap.L().Error("handle INVITE 200 OK", zap.Error(err))
		}
	}

	if bytes.Equal(packet.CSeq.Method, []byte("BYE")) {
		if err := e.handleBye200OK(packet); err != nil {
			zap.L().Error("handle BYE 200 OK", zap.Error(err))
		}
	}

	if isRegisterResponse {
		e.handleRegister200OK(carrier, uaType, packet)
	}
}

func (e *exporter) handleInvite200OK(carrier string, uaType string, packet dto.Packet) error {
	dialogID, err := normalizeDialogID(packet.CallID, packet.From.Tag, packet.To.Tag)
	if err != nil {
		return fmt.Errorf("normalize dialog ID: %w", err)
	}

	expires := packet.SessionExpires
	if expires == 0 {
		expires = 1800
	}
	expiresAt := time.Now().Add(time.Duration(expires) * time.Second)

	zap.L().Debug("create sip dialog",
		zap.String("session", dialogID),
		zap.Int("expires_sec", expires))
	callID := string(packet.CallID)
	e.services.dialoger.Create(dialogID, expiresAt, time.Now(), carrier, uaType, callID)

	// Register RTP media endpoints for correlation: the caller side from the
	// cached INVITE SDP offer, the callee side from this 200 OK SDP answer.
	labels := mediatracker.MediaLabels{Carrier: carrier, UAType: uaType, CallID: callID}
	if offerSDP, ok := e.takeInviteSDP(callID); ok {
		e.registerMediaEndpoints(offerSDP, labels)
	}
	if bytes.Contains(packet.ContentType, []byte("application/sdp")) {
		e.registerMediaEndpoints(packet.Body, labels)
	}
	return nil
}

func (e *exporter) handleBye200OK(packet dto.Packet) error {
	dialogID, err := normalizeDialogID(packet.CallID, packet.From.Tag, packet.To.Tag)
	if err != nil {
		return fmt.Errorf("normalize dialog ID: %w", err)
	}

	zap.L().Debug("delete sip dialog", zap.String("delete session", dialogID))
	result := e.services.dialoger.Delete(dialogID)
	e.mediaTracker.Unregister(string(packet.CallID))
	if result.Duration > 0 {
		e.services.metricser.UpdateSPD(result.Carrier, result.UAType, result.Duration)
		e.services.metricser.SessionCompleted(result.Carrier, result.UAType)
	}
	return nil
}

func (e *exporter) handleRegister200OK(carrier string, uaType string, packet dto.Packet) {
	startTime, ok := e.getRegisterTime(string(packet.CallID))
	if !ok {
		return
	}

	delayMs := float64(time.Since(startTime).Nanoseconds()) / nanosPerMs
	e.services.metricser.UpdateRRD(carrier, uaType, delayMs)
	e.removeRegisterTime(string(packet.CallID))
	zap.L().Debug("RRD measured",
		zap.String("call_id", string(packet.CallID)),
		zap.Float64("delay_ms", delayMs))
}

func splitHeader(line []byte) ([]byte, []byte) {
	i := bytes.IndexByte(line, ':')
	if i == -1 {
		return nil, nil
	}
	return bytes.TrimSpace(line[:i]), bytes.TrimSpace(line[i+1:])
}

func extractTag(value []byte) []byte {
	tagIdx := bytes.Index(value, []byte(";tag="))
	if tagIdx == -1 {
		return nil
	}

	start := tagIdx + tagPrefixLen
	end := start

	for end < len(value) &&
		value[end] != ';' &&
		value[end] != '\r' &&
		value[end] != '\n' &&
		value[end] != ' ' &&
		value[end] != '>' {
		end++
	}

	return value[start:end]
}

func normalizeDialogID(callID, fromTag, toTag []byte) (string, error) {
	if bytes.Equal(fromTag, []byte("")) || bytes.Equal(toTag, []byte("")) {
		return "", fmt.Errorf("from tag or to tag is empty. Call-ID: '%s', From tag: '%s', To tag: '%s'",
			callID, fromTag, toTag)
	}

	var minTag, maxTag []byte
	if bytes.Compare(fromTag, toTag) <= 0 {
		minTag = fromTag
		maxTag = toTag
	} else {
		minTag = toTag
		maxTag = fromTag
	}

	return fmt.Sprintf("%s:%s:%s", callID, minTag, maxTag), nil
}

func htons(i uint16) uint16 {
	return (i<<htonsShift)&htonsMask | i>>htonsShift
}

func extractCSeq(value []byte) ([]byte, []byte) {
	arr := bytes.Split(value, []byte(" "))
	if len(arr) < minSIPParts {
		return nil, nil
	}

	return arr[0], arr[1]
}

func extractSessionExpires(value []byte) int {
	// "1800;refresher=uac" -> 1800
	parts := bytes.Split(value, []byte(";"))
	if len(parts) == 0 {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(string(parts[0]), "%d", &n); err != nil {
		return 0
	}
	return n
}

func (e *exporter) storeRegisterTime(callID string, carrier string, uaType string) {
	e.registerMutex.Lock()
	defer e.registerMutex.Unlock()
	e.registerTracker[callID] = registerEntry{
		timestamp: time.Now(),
		carrier:   carrier,
		uaType:    uaType,
	}
}

func (e *exporter) getRegisterTime(callID string) (time.Time, bool) {
	e.registerMutex.RLock()
	defer e.registerMutex.RUnlock()
	entry, ok := e.registerTracker[callID]
	return entry.timestamp, ok
}

func (e *exporter) removeRegisterTime(callID string) {
	e.registerMutex.Lock()
	defer e.registerMutex.Unlock()
	delete(e.registerTracker, callID)
}

func (e *exporter) storeInviteTime(callID string, carrier string, uaType string) {
	e.inviteMutex.Lock()
	defer e.inviteMutex.Unlock()
	e.inviteTracker[callID] = inviteEntry{
		timestamp: time.Now(),
		carrier:   carrier,
		uaType:    uaType,
	}
}

func (e *exporter) readInviteEntry(callID string) (float64, string, string, bool) {
	e.inviteMutex.Lock()
	defer e.inviteMutex.Unlock()

	entry, ok := e.inviteTracker[callID]
	if !ok {
		return 0, "", defaultUAType, false
	}

	delayMs := float64(time.Since(entry.timestamp).Nanoseconds()) / nanosPerMs

	zap.L().Debug("TTR read",
		zap.String("call_id", callID),
		zap.Float64("delay_ms", delayMs))

	return delayMs, entry.carrier, entry.uaType, true
}

func (e *exporter) markTTRMeasured(callID string) {
	e.inviteMutex.Lock()
	defer e.inviteMutex.Unlock()

	if entry, ok := e.inviteTracker[callID]; ok {
		entry.ttrMeasured = true
		e.inviteTracker[callID] = entry
	}
}

func (e *exporter) isTTRMeasured(callID string) bool {
	e.inviteMutex.RLock()
	defer e.inviteMutex.RUnlock()

	entry, ok := e.inviteTracker[callID]
	if !ok {
		return false
	}
	return entry.ttrMeasured
}

func (e *exporter) markPDDMeasured(callID string) {
	e.inviteMutex.Lock()
	defer e.inviteMutex.Unlock()

	if entry, ok := e.inviteTracker[callID]; ok {
		entry.pddMeasured = true
		e.inviteTracker[callID] = entry
	}
}

func (e *exporter) isPDDMeasured(callID string) bool {
	e.inviteMutex.RLock()
	defer e.inviteMutex.RUnlock()

	entry, ok := e.inviteTracker[callID]
	if !ok {
		return false
	}
	return entry.pddMeasured
}

func (e *exporter) removeInviteTime(callID string) {
	e.inviteMutex.Lock()
	defer e.inviteMutex.Unlock()
	delete(e.inviteTracker, callID)
}

func (e *exporter) getRegisterCarrier(callID string) (string, string, bool) {
	e.registerMutex.RLock()
	defer e.registerMutex.RUnlock()
	entry, ok := e.registerTracker[callID]
	if !ok {
		return "", defaultUAType, false
	}
	return entry.carrier, entry.uaType, true
}

func (e *exporter) getInviteCarrier(callID string) (string, string, bool) {
	e.inviteMutex.RLock()
	defer e.inviteMutex.RUnlock()
	entry, ok := e.inviteTracker[callID]
	if !ok {
		return "", defaultUAType, false
	}
	return entry.carrier, entry.uaType, true
}

// storeInviteSDP caches an INVITE SDP offer keyed by Call-ID so the media
// endpoints can be registered when the 200 OK arrives.
func (e *exporter) storeInviteSDP(callID string, body []byte) {
	e.inviteSDPMutex.Lock()
	defer e.inviteSDPMutex.Unlock()
	e.inviteSDP[callID] = inviteSDPEntity{body: body, timestamp: time.Now()}
}

// takeInviteSDP returns and removes the cached INVITE SDP offer for a Call-ID.
func (e *exporter) takeInviteSDP(callID string) ([]byte, bool) {
	e.inviteSDPMutex.Lock()
	defer e.inviteSDPMutex.Unlock()
	entry, ok := e.inviteSDP[callID]
	if !ok {
		return nil, false
	}
	delete(e.inviteSDP, callID)
	return entry.body, true
}

func (e *exporter) cleanupInviteSDP() {
	e.inviteSDPMutex.Lock()
	defer e.inviteSDPMutex.Unlock()
	now := time.Now()
	for callID, entry := range e.inviteSDP {
		if now.Sub(entry.timestamp) > defaultInviteTTL {
			delete(e.inviteSDP, callID)
		}
	}
}

// registerMediaEndpoints parses an SDP body and registers each audio media
// endpoint in the media tracker under the given dialog labels.
func (e *exporter) registerMediaEndpoints(body []byte, labels mediatracker.MediaLabels) {
	for _, m := range sdp.Parse(body) {
		ml := labels
		ml.SDPCodecs = m.Codecs
		ml.ClockRates = m.ClockRates
		e.mediaTracker.Register(m.IP, m.Port, ml)
		zap.L().Debug("RTP media endpoint registered",
			zap.String("ip", m.IP), zap.Uint16("port", m.Port),
			zap.String("call_id", labels.CallID))
	}
}

func (e *exporter) storeOptionsTime(callID string, carrier string, uaType string) {
	e.optionsMutex.Lock()
	defer e.optionsMutex.Unlock()
	e.optionsTracker[callID] = optionsEntry{
		timestamp: time.Now(),
		carrier:   carrier,
		uaType:    uaType,
	}
}

func (e *exporter) measureOptionsTime(callID string) (float64, string, string, bool) {
	e.optionsMutex.Lock()
	defer e.optionsMutex.Unlock()

	entry, ok := e.optionsTracker[callID]
	if !ok {
		return 0, "", defaultUAType, false
	}

	delete(e.optionsTracker, callID)
	delayMs := float64(time.Since(entry.timestamp).Nanoseconds()) / nanosPerMs

	zap.L().Debug("ORD measured",
		zap.String("call_id", callID),
		zap.Float64("delay_ms", delayMs))

	return delayMs, entry.carrier, entry.uaType, true
}

func (e *exporter) cleanupOptionsTracker() {
	e.optionsMutex.Lock()
	defer e.optionsMutex.Unlock()

	now := time.Now()
	for callID, entry := range e.optionsTracker {
		if now.Sub(entry.timestamp) > defaultOptionsTTL {
			delete(e.optionsTracker, callID)
		}
	}
}
