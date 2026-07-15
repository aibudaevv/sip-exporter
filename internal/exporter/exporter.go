package exporter

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/rlimit"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"

	"github.com/aibudaevv/sip-exporter/internal/carriers"
	"github.com/aibudaevv/sip-exporter/internal/dto"
	"github.com/aibudaevv/sip-exporter/internal/geoip"
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
	ethPAll                   = 0x0003
	readBufSize               = 65536
	defaultRegisterTTL        = 60 * time.Second
	defaultInviteTTL          = 60 * time.Second
	defaultOptionsTTL         = 60 * time.Second
	rtpStreamTTL              = 30 * time.Second // idle RTP stream expiry
	defaultSessionExpiresSec  = 1800             // RFC 4028 default Session-Expires (30 min)
	defaultRegisterExpiresSec = 3600             // RFC 3261 §10.2 default registration expiry (1 hour)

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
	ipV4MinIHL       = 5
	ipV4MaxIHL       = 15
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

	sipPartsCount                = 3
	minSIPParts                  = 2
	minResponseStatusLen         = 3
	tagPrefixLen                 = 5
	nanosPerMs           float64 = 1e6
	htonsShift                   = 8
	htonsMask            uint16  = 0xFF00
	miB                          = 1024 * 1024
	defaultUAType                = "other"
	defaultCarrier               = "other"
	defaultCountry               = "unknown"
)

type (
	timed interface {
		created() time.Time
	}

	registerEntry struct {
		timestamp     time.Time
		carrier       string
		uaType        string
		sourceCountry string
		srcIP         string
	}

	inviteEntry struct {
		timestamp     time.Time
		carrier       string
		uaType        string
		sourceCountry string
		ttrMeasured   bool
		pddMeasured   bool
	}

	optionsEntry struct {
		timestamp     time.Time
		carrier       string
		uaType        string
		sourceCountry string
	}

	registerExpiryEntry struct {
		expiry        time.Time
		carrier       string
		uaType        string
		sourceCountry string
	}

	inviteSDPEntity struct {
		body      []byte
		timestamp time.Time
	}

	exporter struct {
		collection       *ebpf.Collection
		sock             int
		messages         chan []byte
		done             chan struct{}
		wg               sync.WaitGroup
		closeOnce        sync.Once
		sipPort          uint16
		sipsPort         uint16
		services         services
		carrierResolver  *carriers.Resolver
		uaClassifier     *ua.Classifier
		geoip            *geoip.Reader
		localCountryCode string
		hostLabels       bool
		vqHandler        *vq.Handler
		mediaTracker     *mediatracker.Tracker
		// pktSrcIP is written in parseRawPacket and read in handleMessage.
		// Both run synchronously in the readPackets goroutine — no mutex needed.
		// If packet parsing becomes parallel (worker pool), thread srcIP as a
		// parameter instead of using this shared field.
		pktSrcIP              string
		registerScanTracker   *registerScanTracker
		inviteBurstTracker    *inviteBurstTracker
		registerTracker       map[string]registerEntry
		registerMutex         sync.RWMutex
		registerExpiryTracker map[string]registerExpiryEntry
		registerExpiryMutex   sync.RWMutex
		inviteTracker         map[string]inviteEntry
		inviteMutex           sync.RWMutex
		inviteSDP             map[string]inviteSDPEntity
		inviteSDPMutex        sync.Mutex
		optionsTracker        map[string]optionsEntry
		optionsMutex          sync.RWMutex
		initialized           atomic.Bool
	}
	services struct {
		metricser service.Metricser
		dialoger  service.Dialoger
	}
	Deps struct {
		Metricser                 service.Metricser
		Dialoger                  service.Dialoger
		CarrierResolver           *carriers.Resolver
		UAClassifier              *ua.Classifier
		GeoIPReader               *geoip.Reader
		LocalCountryCode          string
		HostLabels                bool
		FraudRegScanThreshold     int
		FraudRegScanWindow        time.Duration
		FraudInviteBurstThreshold int
		FraudInviteBurstWindow    time.Duration
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

func (e registerEntry) created() time.Time   { return e.timestamp }
func (e inviteEntry) created() time.Time     { return e.timestamp }
func (e optionsEntry) created() time.Time    { return e.timestamp }
func (e inviteSDPEntity) created() time.Time { return e.timestamp }

func NewExporter(deps Deps) Exporter {
	e := &exporter{
		services: services{
			metricser: deps.Metricser,
			dialoger:  deps.Dialoger,
		},
		carrierResolver:       deps.CarrierResolver,
		uaClassifier:          deps.UAClassifier,
		geoip:                 deps.GeoIPReader,
		localCountryCode:      deps.LocalCountryCode,
		hostLabels:            deps.HostLabels,
		vqHandler:             vq.NewHandler(deps.Metricser),
		mediaTracker:          mediatracker.NewTracker(rtpStreamTTL),
		registerScanTracker:   newRegisterScanTracker(deps.FraudRegScanThreshold, deps.FraudRegScanWindow),
		inviteBurstTracker:    newInviteBurstTracker(deps.FraudInviteBurstThreshold, deps.FraudInviteBurstWindow),
		messages:              make(chan []byte, messagesChanSize),
		done:                  make(chan struct{}),
		registerTracker:       make(map[string]registerEntry),
		registerExpiryTracker: make(map[string]registerExpiryEntry),
		inviteTracker:         make(map[string]inviteEntry),
		inviteSDP:             make(map[string]inviteSDPEntity),
		optionsTracker:        make(map[string]optionsEntry),
	}
	if e.registerScanTracker == nil {
		zap.L().Warn("fraud register scan detection disabled: threshold and window must be > 0",
			zap.Int("threshold", deps.FraudRegScanThreshold),
			zap.Duration("window", deps.FraudRegScanWindow))
	}
	if e.inviteBurstTracker == nil {
		zap.L().Warn("fraud invite burst detection disabled: threshold and window must be > 0",
			zap.Int("threshold", deps.FraudInviteBurstThreshold),
			zap.Duration("window", deps.FraudInviteBurstWindow))
	}
	return e
}

func checkPrerequisites() error {
	if syscall.Geteuid() != 0 {
		return ErrUserNotRoot
	}
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}
	return nil
}

func (e *exporter) Initialize(
	interfaceName string, path string,
	sipPort, sipsPort int,
	ignoreOutgoing, rtpCapture bool,
	rtpStreamTTL time.Duration,
) error {
	if err := checkPrerequisites(); err != nil {
		return err
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

	e.startWorkers()

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

func (e *exporter) resolveCarrier(ipHeader []byte) (string, string) {
	if e.carrierResolver == nil {
		return defaultCarrier, ""
	}
	srcIP, dstIP := extractIPs(ipHeader)
	carrier, country := e.carrierResolver.Lookup(srcIP)
	if carrier == defaultCarrier {
		carrier, country = e.carrierResolver.Lookup(dstIP)
	}
	return carrier, country
}

func (e *exporter) resolveSourceCountry(carrierCountry string, ipHeader []byte) string {
	if carrierCountry != "" {
		return carrierCountry
	}
	if e.geoip == nil {
		return defaultCountry
	}
	srcIP, _ := extractIPs(ipHeader)
	country, _ := e.geoip.Lookup(srcIP)
	if country == "" {
		return defaultCountry
	}
	return country
}

func (e *exporter) resolveDestinationCountry(toUser []byte) string {
	return geoip.LookupDestination(string(toUser), e.localCountryCode)
}

func (e *exporter) resolveUA(userAgent []byte) string {
	return e.uaClassifier.Classify(userAgent)
}

func (e *exporter) startWorkers() {
	e.wg.Add(1)
	go e.readPackets()
	e.wg.Add(1)
	go e.readSocket()
	e.wg.Add(1)
	go e.sipDialogMetricsUpdate()
}

func (e *exporter) sipDialogMetricsUpdate() {
	defer e.wg.Done()
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
		e.cleanupExpiredRegistrations()
		e.registerScanTracker.cleanup()
		e.inviteBurstTracker.cleanup()
		e.cleanupInviteTracker()
		e.cleanupInviteSDP()
		e.cleanupOptionsTracker()
		e.mediaTracker.Cleanup()
		s := e.services.dialoger.Size()

		for _, r := range results {
			e.services.metricser.SessionCompleted(r.Carrier, r.UAType, r.SourceCountry)
			e.services.metricser.UpdateSPD(r.Carrier, r.UAType, r.SourceCountry, r.Duration)
			rtpResult := e.mediaTracker.Unregister(r.CallID)
			e.handleRTPDialogResult(rtpResult, r.Carrier, r.UAType, r.SourceCountry)
		}

		zap.L().Debug("update metrics", zap.Int("size dialogs", s), zap.Int("expired", len(results)))

		e.services.metricser.UpdateSessions(e.services.dialoger.Counts())
		e.services.metricser.UpdateActiveRegistrations(e.registrationCounts())

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

func cleanupExpired[V timed](mu sync.Locker, m map[string]V, ttl time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	now := time.Now()
	for k, v := range m {
		if now.Sub(v.created()) > ttl {
			delete(m, k)
		}
	}
}

func (e *exporter) cleanupRegisterTracker() {
	cleanupExpired(&e.registerMutex, e.registerTracker, defaultRegisterTTL)
}

func (e *exporter) cleanupInviteTracker() {
	cleanupExpired(&e.inviteMutex, e.inviteTracker, defaultInviteTTL)
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
	defer e.wg.Done()
	for {
		select {
		case <-e.done:
			return
		case packet, ok := <-e.messages:
			if !ok {
				return
			}
			if errType, err := e.parseRawPacket(packet); err != nil {
				e.services.metricser.SystemError()
				e.services.metricser.ParseError(errType)
				zap.L().Error("parse err", zap.Error(err))
			}
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
		e.services.metricser.RTPDropped()
	}
	return true
}

// isSIPPacket does a quick L4 port check to classify a packet as SIP (port
// 5060/5061) or RTP/other. When headers can't be parsed, defaults to true
// to avoid dropping potentially critical traffic.
func (e *exporter) isSIPPacket(packet []byte) bool {
	if len(packet) < minRawPacketLen {
		return true
	}
	offset := ethHeaderLen
	if packet[12] == vlanEthTypeHi && packet[13] == vlanEthTypeLo {
		offset = vlanHeaderLen
	}
	ihl := int(packet[offset] & ipV4HdrLenMask)
	if ihl < ipV4MinIHL || ihl > ipV4MaxIHL {
		return true
	}
	udpOff := offset + ihl*ipV4HdrLenShift
	if len(packet) < udpOff+udpHeaderLen {
		return true
	}
	srcPort := binary.BigEndian.Uint16(packet[udpOff : udpOff+2])
	dstPort := binary.BigEndian.Uint16(packet[udpOff+2 : udpOff+4])
	return srcPort == e.sipPort || srcPort == e.sipsPort ||
		dstPort == e.sipPort || dstPort == e.sipsPort
}

// parseEthernet extracts the IP offset from the L2 Ethernet header,
// handling VLAN (802.1Q). Returns ipOffset and the ethType bytes.
func parseEthernet(packet []byte) (int, []byte, error) {
	if len(packet) < minRawPacketLen {
		return 0, nil, fmt.Errorf("wrong len packet %d", len(packet))
	}
	ethType := packet[12:14]
	ipOffset := ethHeaderLen
	if ethType[0] == vlanEthTypeHi && ethType[1] == vlanEthTypeLo {
		if len(packet) < minVLANPacketLen {
			return 0, nil, fmt.Errorf("wrong len packet %d", len(packet))
		}
		ethType = packet[16:18]
		ipOffset = vlanHeaderLen
	}
	return ipOffset, ethType, nil
}

// parseIPv4Header validates the IPv4 header and returns the header slice
// and its computed length (IHL * 4).
func parseIPv4Header(packet []byte, ipOffset int) ([]byte, int, error) {
	if len(packet) < ipOffset+ipV4MinHeaderLen {
		return nil, 0, errors.New("ip header too short")
	}
	ipHeader := packet[ipOffset : ipOffset+ipV4MinHeaderLen]
	ihl := ipHeader[0] & ipV4HdrLenMask
	if ihl < ipV4MinIHL || ihl > ipV4MaxIHL {
		return nil, 0, fmt.Errorf("invalid IHL: %d", ihl)
	}
	headerLen := int(ihl) * ipV4HdrLenShift
	return ipHeader, headerLen, nil
}

// parseUDPOffset validates the UDP header fits and returns its offset.
func parseUDPOffset(packet []byte, ipOffset, ipHeaderLen int) (int, error) {
	udpOffset := ipOffset + ipHeaderLen
	if len(packet) < udpOffset+udpHeaderLen {
		return 0, errors.New("udp header too short")
	}
	return udpOffset, nil
}

// isSIPMethod checks if data starts with a known SIP method or SIP/2.0,
// followed by a space delimiter (prevents prefix collisions like INFORMATIONAL).
func isSIPMethod(data []byte) bool {
	return hasMethodPrefix(data, []byte("INVITE")) ||
		hasMethodPrefix(data, []byte("ACK")) ||
		hasMethodPrefix(data, []byte("BYE")) ||
		hasMethodPrefix(data, []byte("CANCEL")) ||
		hasMethodPrefix(data, []byte("OPTIONS")) ||
		hasMethodPrefix(data, []byte("REGISTER")) ||
		hasMethodPrefix(data, []byte("SUBSCRIBE")) ||
		hasMethodPrefix(data, []byte("NOTIFY")) ||
		hasMethodPrefix(data, []byte("PUBLISH")) ||
		hasMethodPrefix(data, []byte("INFO")) ||
		hasMethodPrefix(data, []byte("PRACK")) ||
		hasMethodPrefix(data, []byte("UPDATE")) ||
		hasMethodPrefix(data, []byte("MESSAGE")) ||
		hasMethodPrefix(data, []byte("REFER")) ||
		hasMethodPrefix(data, []byte("SIP/2.0"))
}

func hasMethodPrefix(data, method []byte) bool {
	return bytes.HasPrefix(data, method) &&
		len(data) > len(method) &&
		data[len(method)] == ' '
}

func isSDPContentType(contentType []byte) bool {
	return bytes.Contains(bytes.ToLower(contentType), []byte("application/sdp"))
}

func isVQContentType(contentType []byte) bool {
	return bytes.Contains(bytes.ToLower(contentType), []byte("application/vq-rtcpxr"))
}

// parseRawPacket parses raw L2 packet. Returns error type (l2, l3, l4, sip) and error.
func (e *exporter) parseRawPacket(packet []byte) (string, error) {
	ipOffset, ethType, err := parseEthernet(packet)
	if err != nil {
		return parseErrTypeL2, err
	}

	if ethType[0] != ethTypeIPv4Hi || ethType[1] != ethTypeIPv4Lo {
		return parseErrTypeL3, errors.New("not IPv4 packet")
	}

	ipHeader, ipHeaderLen, err := parseIPv4Header(packet, ipOffset)
	if err != nil {
		return parseErrTypeL3, err
	}

	carrier, carrierCountry := e.resolveCarrier(ipHeader)
	sourceCountry := e.resolveSourceCountry(carrierCountry, ipHeader)
	e.pktSrcIP = net.IPv4(ipHeader[12], ipHeader[13], ipHeader[14], ipHeader[15]).String()

	if ipHeader[9] != ipProtoUDP {
		return parseErrTypeL4, errors.New("not UDP packet")
	}

	udpOffset, err := parseUDPOffset(packet, ipOffset, ipHeaderLen)
	if err != nil {
		return parseErrTypeL4, err
	}

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

	if len(sipData) < minSIPDataLen {
		return parseErrTypeSIP, fmt.Errorf("packet too small for SIP: %d", len(sipData))
	}

	if !isSIPMethod(sipData) {
		return parseErrTypeSIP, errors.New("not a SIP packet")
	}

	zap.L().Debug("packet raw", zap.ByteString("sip_data", sipData))

	err = e.handleMessage(carrier, sourceCountry, sipData)
	if err != nil {
		return parseErrTypeSIP, err
	}

	return "", nil
}

func (e *exporter) sipPacketParse(raw []byte) (dto.Packet, error) {
	lines := bytes.Split(raw, []byte("\r\n"))
	if len(lines) == 0 {
		return dto.Packet{}, fmt.Errorf("split return empty result, raw: %q", raw)
	}

	lines = unfoldHeaders(lines)

	p := dto.Packet{}
	if bytes.HasPrefix(lines[0], []byte("SIP/2.0")) {
		p.IsResponse = true
		rest := bytes.TrimPrefix(lines[0], []byte("SIP/2.0 "))
		if len(rest) < minResponseStatusLen {
			return dto.Packet{}, fmt.Errorf("malformed status line: %q", lines[0])
		}
		p.ResponseStatus = rest[:minResponseStatusLen]
	} else {
		parts := bytes.SplitN(lines[0], []byte(" "), sipPartsCount)
		if len(parts) < minSIPParts {
			return dto.Packet{}, fmt.Errorf("malformed request line: %q", lines[0])
		}
		p.IsResponse = false
		p.Method = bytes.TrimSpace(parts[0])
	}

	if err := e.parseHeaders(lines, &p); err != nil {
		return dto.Packet{}, err
	}

	if p.CallID == nil {
		return dto.Packet{}, errors.New("missing Call-ID header")
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
		header = normalizeHeaderName(header)

		switch {
		case bytes.EqualFold(header, []byte("From")):
			tag := extractTag(value)
			if tag == nil {
				return fmt.Errorf("fail extract tag from '%s'", value)
			}

			p.From.Tag = tag
			p.From.User, p.From.Addr = ParseURI(value)
		case bytes.EqualFold(header, []byte("To")):
			p.To.Tag = extractTag(value)
			p.To.User, p.To.Addr = ParseURI(value)
		case bytes.EqualFold(header, []byte("Call-ID")):
			p.CallID = value
		case bytes.EqualFold(header, []byte("CSeq")):
			id, method := extractCSeq(value)
			if id == nil || method == nil {
				return fmt.Errorf("fail extract CSeq from '%s'", value)
			}

			p.CSeq.Method = method
			p.CSeq.ID = id
		case bytes.EqualFold(header, []byte("Session-Expires")):
			p.SessionExpires = extractSessionExpires(value)
		case bytes.EqualFold(header, []byte("Expires")):
			p.Expires = extractExpires(value)
		case bytes.EqualFold(header, []byte("User-Agent")):
			if p.UserAgent == nil {
				p.UserAgent = value
			}
		case bytes.EqualFold(header, []byte("Content-Type")):
			if p.ContentType == nil {
				p.ContentType = value
			}
		}
	}

	return nil
}

func (e *exporter) handleMessage(carrier string, sourceCountry string, rawPacket []byte) error {
	packet, err := e.sipPacketParse(rawPacket)
	if err != nil {
		return fmt.Errorf("parse SIP packet: %w", err)
	}

	packet.SourceIP = e.pktSrcIP

	zap.L().Debug("parsed packet", zap.Any("packet", packet))

	uaType := e.resolveUA(packet.UserAgent)

	if packet.IsResponse {
		e.handleResponse(carrier, uaType, sourceCountry, packet)
	} else {
		e.handleRequest(carrier, uaType, sourceCountry, packet)
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
		e.services.metricser.UpdateRTPPackets(res.Carrier, res.UAType, res.Codec, res.SourceCountry)
	}
	if res.Duplicate {
		e.services.metricser.UpdateRTPDuplicates(res.Carrier, res.UAType, res.Codec, res.SourceCountry)
	}
	if res.Lost > 0 {
		e.services.metricser.UpdateRTPLoss(res.Carrier, res.UAType, res.Codec, res.SourceCountry, res.Lost)
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
	type aggKey struct{ carrier, uaType, codec, sourceCountry string }
	tmp := make(map[aggKey]int)
	for _, s := range stats {
		e.services.metricser.UpdateRTPJitter(s.Carrier, s.UAType, s.Codec, s.SourceCountry, s.JitterMs)
		e.services.metricser.UpdateRTPMOS(s.Carrier, s.UAType, s.Codec, s.SourceCountry, s.MOS)
		e.services.metricser.UpdateRTPMOSVariants(
			s.Carrier, s.UAType, s.Codec, s.SourceCountry,
			s.MOSF1, s.MOSF2, s.MOSAdaptive,
		)
		e.services.metricser.UpdateRTPRFactor(s.Carrier, s.UAType, s.Codec, s.SourceCountry, s.RFactor)
		e.services.metricser.UpdateRTPLossDistribution(
			s.Carrier, s.UAType, s.Codec, s.SourceCountry,
			s.BurstLossDensity, s.GapLossDensity,
		)
		tmp[aggKey{s.Carrier, s.UAType, s.Codec, s.SourceCountry}]++
	}
	counts := make([]service.LabeledCount, 0, len(tmp))
	for k, n := range tmp {
		counts = append(counts, service.LabeledCount{
			Labels: map[string]string{
				"carrier":        k.carrier,
				"ua_type":        k.uaType,
				"codec":          k.codec,
				"source_country": k.sourceCountry,
			},
			Count: n,
		})
	}
	e.services.metricser.UpdateRTPActiveStreams(counts)
}

func (e *exporter) handleRTPDialogResult(
	r mediatracker.RTPDialogResult,
	carrier, uaType, sourceCountry string,
) {
	if r.MediaExpected && !r.RTPObserved {
		e.services.metricser.MissingRTP(carrier, uaType, sourceCountry)
	}
	if r.OneWay {
		e.services.metricser.OneWayCall(carrier, uaType, sourceCountry)
	}
}

func (e *exporter) handleRequest(carrier string, uaType string, sourceCountry string, packet dto.Packet) {
	var destinationCountry, callerHost, calledHost string
	isReinvite := false
	isRetransmission := false
	if bytes.Equal(packet.Method, []byte("INVITE")) {
		destinationCountry = e.resolveDestinationCountry(packet.To.User)
		if e.hostLabels {
			callerHost = string(packet.From.Addr)
			calledHost = string(packet.To.Addr)
		}
		if dialogID, err := normalizeDialogID(packet.CallID, packet.From.Tag, packet.To.Tag); err == nil &&
			e.services.dialoger.HasActiveDialog(dialogID) {
			isReinvite = true
		}
		if !isReinvite && e.hasInviteTracker(string(packet.CallID)) {
			isRetransmission = true
		}
	}

	if isReinvite {
		e.services.metricser.Reinvite(carrier, uaType, sourceCountry)
	} else if !isRetransmission {
		e.services.metricser.Request(
			carrier, uaType, sourceCountry, destinationCountry,
			callerHost, calledHost, packet.Method,
		)
	}

	switch {
	case bytes.Equal(packet.Method, []byte("REGISTER")):
		e.storeRegisterTime(string(packet.CallID), carrier, uaType, sourceCountry, packet.SourceIP)
	case bytes.Equal(packet.Method, []byte("INVITE")):
		if !isReinvite && !isRetransmission {
			e.storeInviteTime(string(packet.CallID), carrier, uaType, sourceCountry)
			e.inviteBurstTracker.record(packet.SourceIP, carrier, sourceCountry, e.services.metricser)
		}
		if isSDPContentType(packet.ContentType) {
			e.storeInviteSDP(string(packet.CallID), packet.Body)
		}
	case bytes.Equal(packet.Method, []byte("CANCEL")):
		e.removeInviteTime(string(packet.CallID))
	case bytes.Equal(packet.Method, []byte("OPTIONS")):
		e.storeOptionsTime(string(packet.CallID), carrier, uaType, sourceCountry)
	}

	if isVQContentType(packet.ContentType) {
		e.vqHandler.HandleVQReport(packet.Body, carrier, uaType, sourceCountry)
	}
}

func (e *exporter) handleResponse(
	packetCarrier, packetUAType, packetSourceCountry string,
	packet dto.Packet,
) {
	isInviteResponse := bytes.Equal(packet.CSeq.Method, []byte("INVITE"))
	isRegisterResponse := bytes.Equal(packet.CSeq.Method, []byte("REGISTER"))
	is200OK := bytes.Equal(packet.ResponseStatus, []byte("200"))

	carrier := packetCarrier
	uaType := packetUAType
	sourceCountry := packetSourceCountry

	if isInviteResponse {
		carrier, uaType, sourceCountry = e.handleInviteResponse(carrier, uaType, sourceCountry, packet)
	}

	if isRegisterResponse {
		if regCarrier, regUAType, regSC, regSrcIP, ok := e.getRegisterCarrier(string(packet.CallID)); ok {
			carrier = regCarrier
			uaType = regUAType
			sourceCountry = regSC
			packet.SourceIP = regSrcIP
		}
	}

	isOptionsResponse := bytes.Equal(packet.CSeq.Method, []byte("OPTIONS"))
	if isOptionsResponse {
		if delayMs, optCarrier, optUAType, optSC, ok := e.measureOptionsTime(string(packet.CallID)); ok {
			e.services.metricser.UpdateORD(optCarrier, optUAType, optSC, delayMs)
		}
	}

	// Detect re-INVITE response: dialog already exists for this INVITE.
	// re-INVITE responses must not contaminate SER/SEER/ISA/SCR/ASR/NER
	// atomic counters, since the INVITE itself was not counted in inviteTotal.
	// This also deduplicates 200 OK retransmissions (Timer G on UDP): after
	// the dialog is created by the first 200 OK, subsequent retransmissions
	// hit the same HasActiveDialog check and are excluded from ratio counters.
	isReinviteResponse := false
	if isInviteResponse {
		if dialogID, err := normalizeDialogID(packet.CallID, packet.From.Tag, packet.To.Tag); err == nil &&
			e.services.dialoger.HasActiveDialog(dialogID) {
			isReinviteResponse = true
		}
	}

	e.services.metricser.ResponseWithMetrics(
		carrier,
		uaType,
		sourceCountry,
		packet.ResponseStatus,
		isInviteResponse && !isReinviteResponse,
		is200OK,
	)

	if is200OK {
		e.handle200OKResponse(carrier, uaType, sourceCountry, packet, isRegisterResponse, isReinviteResponse)
	} else if isRegisterResponse {
		e.handleRegisterNon200Response(carrier, uaType, sourceCountry, packet)
	}
}

func (e *exporter) handleInviteResponse(
	fallbackCarrier string,
	fallbackUAType string,
	fallbackSourceCountry string,
	packet dto.Packet,
) (string, string, string) {
	carrier := fallbackCarrier
	uaType := fallbackUAType
	sourceCountry := fallbackSourceCountry
	if inviteCarrier, inviteUAType, inviteSC, ok := e.getInviteCarrier(string(packet.CallID)); ok {
		carrier = inviteCarrier
		uaType = inviteUAType
		sourceCountry = inviteSC
	}
	if len(packet.ResponseStatus) > 0 {
		if packet.ResponseStatus[0] == '1' {
			e.handleProvisionalResponse(packet, sourceCountry)
		} else {
			e.removeInviteTime(string(packet.CallID))
		}
	}
	return carrier, uaType, sourceCountry
}

func (e *exporter) handleProvisionalResponse(packet dto.Packet, _ string) {
	delayMs, inviteCarrier, inviteUAType, inviteSC, ok := e.readInviteEntry(string(packet.CallID))
	if !ok {
		return
	}
	callID := string(packet.CallID)
	if !e.isTTRMeasured(callID) {
		e.services.metricser.UpdateTTR(inviteCarrier, inviteUAType, inviteSC, delayMs)
		e.markTTRMeasured(callID)
	}
	if !e.isPDDMeasured(callID) {
		e.measurePDD(inviteCarrier, inviteUAType, inviteSC, delayMs, packet.ResponseStatus, callID)
	}
}

func (e *exporter) measurePDD(
	carrier, uaType, sourceCountry string,
	delayMs float64, status []byte, callID string,
) {
	if len(status) >= 3 && status[1] == '8' && status[2] == '0' {
		e.services.metricser.UpdatePDD(carrier, uaType, sourceCountry, delayMs)
		e.markPDDMeasured(callID)
	}
}

func (e *exporter) handleRegisterNon200Response(
	carrier, uaType, sourceCountry string, packet dto.Packet,
) {
	if len(packet.ResponseStatus) > 0 && packet.ResponseStatus[0] >= '3' {
		e.services.metricser.RegisterFailure(carrier, uaType, sourceCountry, string(packet.ResponseStatus))
	}

	if len(packet.ResponseStatus) > 0 && packet.ResponseStatus[0] == '3' {
		if startTime, ok := e.getRegisterTime(string(packet.CallID)); ok {
			delayMs := float64(time.Since(startTime).Nanoseconds()) / nanosPerMs
			e.services.metricser.UpdateLRD(carrier, uaType, sourceCountry, delayMs)
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

func (e *exporter) handle200OKResponse(
	carrier, uaType, sourceCountry string,
	packet dto.Packet, isRegisterResponse, isReinvite bool,
) {
	zap.L().Debug("handle message", zap.ByteString("200 OK cseq method", packet.CSeq.Method))

	if bytes.Equal(packet.CSeq.Method, []byte("INVITE")) {
		if !isReinvite {
			destinationCountry := e.resolveDestinationCountry(packet.To.User)
			var callerHost, calledHost string
			if e.hostLabels {
				callerHost = string(packet.From.Addr)
				calledHost = string(packet.To.Addr)
			}
			e.services.metricser.Invite200OK(carrier, uaType, sourceCountry, destinationCountry, callerHost, calledHost)
		}
		if err := e.handleInvite200OK(carrier, uaType, sourceCountry, packet, isReinvite); err != nil {
			zap.L().Error("handle INVITE 200 OK", zap.Error(err))
		}
	}

	if bytes.Equal(packet.CSeq.Method, []byte("BYE")) {
		if err := e.handleBye200OK(packet, sourceCountry); err != nil {
			zap.L().Error("handle BYE 200 OK", zap.Error(err))
		}
	}

	if isRegisterResponse {
		e.handleRegister200OK(carrier, uaType, sourceCountry, packet)
	}
}

func (e *exporter) handleInvite200OK(
	carrier, uaType, sourceCountry string,
	packet dto.Packet, isReinvite bool,
) error {
	dialogID, err := normalizeDialogID(packet.CallID, packet.From.Tag, packet.To.Tag)
	if err != nil {
		return fmt.Errorf("normalize dialog ID: %w", err)
	}

	expires := packet.SessionExpires
	if expires == 0 {
		expires = defaultSessionExpiresSec
	}
	expiresAt := time.Now().Add(time.Duration(expires) * time.Second)

	callID := string(packet.CallID)
	if isReinvite {
		zap.L().Debug("refresh sip dialog (re-INVITE)",
			zap.String("session", dialogID),
			zap.Int("expires_sec", expires))
		e.services.dialoger.Refresh(dialogID, expiresAt)
	} else {
		zap.L().Debug("create sip dialog",
			zap.String("session", dialogID),
			zap.Int("expires_sec", expires))
		e.services.dialoger.Create(dialogID, expiresAt, time.Now(), carrier, uaType, sourceCountry, callID)
	}

	// Register RTP media endpoints for correlation: the caller side from the
	// cached INVITE SDP offer, the callee side from this 200 OK SDP answer.
	labels := mediatracker.MediaLabels{
		Carrier: carrier, UAType: uaType, SourceCountry: sourceCountry, CallID: callID,
	}
	if offerSDP, ok := e.takeInviteSDP(callID); ok {
		e.registerMediaEndpoints(offerSDP, labels)
	}
	if isSDPContentType(packet.ContentType) {
		e.registerMediaEndpoints(packet.Body, labels)
	}
	return nil
}

func (e *exporter) handleBye200OK(packet dto.Packet, _ string) error {
	dialogID, err := normalizeDialogID(packet.CallID, packet.From.Tag, packet.To.Tag)
	if err != nil {
		return fmt.Errorf("normalize dialog ID: %w", err)
	}

	zap.L().Debug("delete sip dialog", zap.String("delete session", dialogID))
	result := e.services.dialoger.Delete(dialogID)
	rtpResult := e.mediaTracker.Unregister(string(packet.CallID))
	e.handleRTPDialogResult(rtpResult, result.Carrier, result.UAType, result.SourceCountry)
	if result.Duration > 0 {
		e.services.metricser.UpdateSPD(result.Carrier, result.UAType, result.SourceCountry, result.Duration)
		e.services.metricser.SessionCompleted(result.Carrier, result.UAType, result.SourceCountry)
	}
	return nil
}

func (e *exporter) handleRegister200OK(carrier string, uaType string, sourceCountry string, packet dto.Packet) {
	e.services.metricser.RegisterSuccess(carrier, uaType, sourceCountry)

	expires := packet.Expires
	if expires <= 0 {
		expires = defaultRegisterExpiresSec
	}
	aor := string(packet.From.User) + "@" + string(packet.From.Addr)
	e.storeRegistration(aor, carrier, uaType, sourceCountry, packet.SourceIP, expires)

	startTime, ok := e.getRegisterTime(string(packet.CallID))
	if !ok {
		return
	}

	delayMs := float64(time.Since(startTime).Nanoseconds()) / nanosPerMs
	e.services.metricser.UpdateRRD(carrier, uaType, sourceCountry, delayMs)
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

func normalizeHeaderName(header []byte) []byte {
	if len(header) != 1 {
		return header
	}
	switch header[0] {
	case 'f', 'F':
		return []byte("From")
	case 't', 'T':
		return []byte("To")
	case 'i', 'I':
		return []byte("Call-ID")
	case 'c', 'C':
		return []byte("Content-Type")
	}
	return header
}

func unfoldHeaders(lines [][]byte) [][]byte {
	var result [][]byte
	for _, line := range lines {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if len(result) > 0 {
				result[len(result)-1] = append(result[len(result)-1], bytes.TrimLeft(line, " \t")...)
			}
			continue
		}
		result = append(result, line)
	}
	return result
}

func extractTag(value []byte) []byte {
	searchStart := 0
	if ltIdx := bytes.IndexByte(value, '<'); ltIdx != -1 {
		if gtIdx := bytes.IndexByte(value[ltIdx:], '>'); gtIdx != -1 {
			searchStart = ltIdx + gtIdx
		}
	}

	tagIdx := bytes.Index(bytes.ToLower(value[searchStart:]), []byte(";tag="))
	if tagIdx == -1 {
		return nil
	}
	tagIdx += searchStart

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
	arr := bytes.Fields(value)
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
	n, err := strconv.Atoi(string(bytes.TrimSpace(parts[0])))
	if err != nil {
		return 0
	}
	return n
}

func extractExpires(value []byte) int {
	// "3600" -> 3600 (RFC 3261 §20.19 delta-seconds; no params unlike Session-Expires)
	n, err := strconv.Atoi(string(bytes.TrimSpace(value)))
	if err != nil {
		return 0
	}
	return n
}

func (e *exporter) storeRegisterTime(callID, carrier, uaType, sourceCountry, srcIP string) {
	e.registerMutex.Lock()
	defer e.registerMutex.Unlock()
	e.registerTracker[callID] = registerEntry{
		timestamp:     time.Now(),
		carrier:       carrier,
		uaType:        uaType,
		sourceCountry: sourceCountry,
		srcIP:         srcIP,
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

// storeRegistration records or refreshes an active registration keyed by its
// Address-of-Record (the SIP URI). A refresh of an existing AOR overwrites the
// entry (extending its TTL) instead of creating a duplicate.
func (e *exporter) storeRegistration(aor, carrier, uaType, sourceCountry, srcIP string, expiresSec int) {
	expiry := time.Now().Add(time.Duration(expiresSec) * time.Second)
	countryChanged := false
	e.registerExpiryMutex.Lock()
	if e.registerExpiryTracker == nil {
		e.registerExpiryTracker = make(map[string]registerExpiryEntry)
	}
	if prev, ok := e.registerExpiryTracker[aor]; ok && prev.sourceCountry != "" &&
		prev.sourceCountry != sourceCountry {
		countryChanged = true
	}
	e.registerExpiryTracker[aor] = registerExpiryEntry{
		expiry:        expiry,
		carrier:       carrier,
		uaType:        uaType,
		sourceCountry: sourceCountry,
	}
	e.registerExpiryMutex.Unlock()

	if countryChanged {
		e.services.metricser.RegisterCountryChange(carrier, sourceCountry)
	}
	e.registerScanTracker.record(srcIP, aor, carrier, sourceCountry, e.services.metricser)
}

func (e *exporter) cleanupExpiredRegistrations() {
	e.registerExpiryMutex.Lock()
	defer e.registerExpiryMutex.Unlock()
	now := time.Now()
	for aor, entry := range e.registerExpiryTracker {
		if now.After(entry.expiry) {
			delete(e.registerExpiryTracker, aor)
		}
	}
}

// registrationCounts aggregates active registrations by carrier/ua_type/source_country.
func (e *exporter) registrationCounts() []service.LabeledCount {
	e.registerExpiryMutex.RLock()
	defer e.registerExpiryMutex.RUnlock()
	type labelKey struct {
		carrier, uaType, sourceCountry string
	}
	counts := make(map[labelKey]int, len(e.registerExpiryTracker))
	for _, entry := range e.registerExpiryTracker {
		k := labelKey{entry.carrier, entry.uaType, entry.sourceCountry}
		counts[k]++
	}
	result := make([]service.LabeledCount, 0, len(counts))
	for k, n := range counts {
		result = append(result, service.LabeledCount{
			Labels: map[string]string{
				"carrier":        k.carrier,
				"ua_type":        k.uaType,
				"source_country": k.sourceCountry,
			},
			Count: n,
		})
	}
	return result
}

func (e *exporter) storeInviteTime(callID, carrier, uaType, sourceCountry string) {
	e.inviteMutex.Lock()
	defer e.inviteMutex.Unlock()
	e.inviteTracker[callID] = inviteEntry{
		timestamp:     time.Now(),
		carrier:       carrier,
		uaType:        uaType,
		sourceCountry: sourceCountry,
	}
}

func (e *exporter) readInviteEntry(callID string) (float64, string, string, string, bool) {
	e.inviteMutex.Lock()
	defer e.inviteMutex.Unlock()

	entry, ok := e.inviteTracker[callID]
	if !ok {
		return 0, "", defaultUAType, defaultCountry, false
	}

	delayMs := float64(time.Since(entry.timestamp).Nanoseconds()) / nanosPerMs

	zap.L().Debug("TTR read",
		zap.String("call_id", callID),
		zap.Float64("delay_ms", delayMs))

	return delayMs, entry.carrier, entry.uaType, entry.sourceCountry, true
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

func (e *exporter) hasInviteTracker(callID string) bool {
	e.inviteMutex.RLock()
	defer e.inviteMutex.RUnlock()
	_, ok := e.inviteTracker[callID]
	return ok
}

func (e *exporter) getRegisterCarrier(callID string) (string, string, string, string, bool) {
	e.registerMutex.RLock()
	defer e.registerMutex.RUnlock()
	entry, ok := e.registerTracker[callID]
	if !ok {
		return "", defaultUAType, defaultCountry, "", false
	}
	return entry.carrier, entry.uaType, entry.sourceCountry, entry.srcIP, true
}

func (e *exporter) getInviteCarrier(callID string) (string, string, string, bool) {
	e.inviteMutex.RLock()
	defer e.inviteMutex.RUnlock()
	entry, ok := e.inviteTracker[callID]
	if !ok {
		return "", defaultUAType, defaultCountry, false
	}
	return entry.carrier, entry.uaType, entry.sourceCountry, true
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
	cleanupExpired(&e.inviteSDPMutex, e.inviteSDP, defaultInviteTTL)
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

func (e *exporter) storeOptionsTime(callID, carrier, uaType, sourceCountry string) {
	e.optionsMutex.Lock()
	defer e.optionsMutex.Unlock()
	e.optionsTracker[callID] = optionsEntry{
		timestamp:     time.Now(),
		carrier:       carrier,
		uaType:        uaType,
		sourceCountry: sourceCountry,
	}
}

func (e *exporter) measureOptionsTime(callID string) (float64, string, string, string, bool) {
	e.optionsMutex.Lock()
	defer e.optionsMutex.Unlock()

	entry, ok := e.optionsTracker[callID]
	if !ok {
		return 0, "", defaultUAType, defaultCountry, false
	}

	delete(e.optionsTracker, callID)
	delayMs := float64(time.Since(entry.timestamp).Nanoseconds()) / nanosPerMs

	zap.L().Debug("ORD measured",
		zap.String("call_id", callID),
		zap.Float64("delay_ms", delayMs))

	return delayMs, entry.carrier, entry.uaType, entry.sourceCountry, true
}

func (e *exporter) cleanupOptionsTracker() {
	cleanupExpired(&e.optionsMutex, e.optionsTracker, defaultOptionsTTL)
}
