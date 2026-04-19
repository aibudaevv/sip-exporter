package exporter

import (
	"bytes"
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

	"gitlab.com/sip-exporter/internal/dto"
	"gitlab.com/sip-exporter/internal/service"
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
)

type (
	registerEntry struct {
		timestamp time.Time
	}

	inviteEntry struct {
		timestamp time.Time
	}

	optionsEntry struct {
		timestamp time.Time
	}

	exporter struct {
		collection      *ebpf.Collection
		sock            int
		messages        chan []byte
		services        services
		registerTracker map[string]registerEntry
		registerMutex   sync.RWMutex
		inviteTracker   map[string]inviteEntry
		inviteMutex     sync.RWMutex
		optionsTracker  map[string]optionsEntry
		optionsMutex    sync.RWMutex
		initialized     atomic.Bool
	}
	services struct {
		metricser service.Metricser
		dialoger  service.Dialoger
	}
	Exporter interface {
		Initialize(interfaceName string, path string, sipPort, sipsPort int) error
		IsAlive() bool
		Close()
	}
)

func NewExporter(m service.Metricser, d service.Dialoger) Exporter {
	return &exporter{
		services: services{
			metricser: m,
			dialoger:  d,
		},
		messages:        make(chan []byte, 10_000),
		registerTracker: make(map[string]registerEntry),
		inviteTracker:   make(map[string]inviteEntry),
		optionsTracker:  make(map[string]optionsEntry),
	}
}

func (e *exporter) Initialize(interfaceName string, path string, sipPort, sipsPort int) error {
	if syscall.Geteuid() != 0 {
		return ErrUserNotRoot
	}

	collection, err := ebpf.LoadCollection(path)
	if err != nil {
		return fmt.Errorf("failed to load BPF collection: %w", err)
	}

	e.collection = collection

	prog := collection.Programs["bpf_socket_filter"]
	if prog == nil {
		return errors.New("failed to find BPF program: bpf_socket_filter")
	}

	// Configure SIP ports in eBPF map
	sipPortsMap := collection.Maps["sip_ports"]
	if sipPortsMap == nil {
		return errors.New("failed to find sip_ports map")
	}

	keySIP := uint32(0)
	keySIPS := uint32(1)
	if err := sipPortsMap.Update(keySIP, uint16(sipPort), ebpf.UpdateAny); err != nil {
		return fmt.Errorf("failed to set SIP port: %w", err)
	}
	if err := sipPortsMap.Update(keySIPS, uint16(sipsPort), ebpf.UpdateAny); err != nil {
		return fmt.Errorf("failed to set SIPS port: %w", err)
	}

	// Create AF_PACKET socket with SOCK_RAW
	sock, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(ethPAll)))
	if err != nil {
		return fmt.Errorf("failed to create AF_PACKET socket: %w", err)
	}
	e.sock = sock

	socketRecvBufSize := 4 * 1024 * 1024
	if setErr := unix.SetsockoptInt(sock, unix.SOL_SOCKET, unix.SO_RCVBUF, socketRecvBufSize); setErr != nil {
		return fmt.Errorf("failed to set SO_RCVBUF: %w", setErr)
	}

	var actualBufSize int
	actualBufSize, err = unix.GetsockoptInt(sock, unix.SOL_SOCKET, unix.SO_RCVBUF)
	if err != nil {
		return fmt.Errorf("failed to get SO_RCVBUF: %w", err)
	}
	zap.L().Info("socket receive buffer configured",
		zap.Int("requested_bytes", socketRecvBufSize),
		zap.Int("actual_bytes", actualBufSize))

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
	go e.readSocket()
	go e.sipDialogMetricsUpdate()

	e.initialized.Store(true)

	return nil
}

func (e *exporter) sipDialogMetricsUpdate() {
	ticker := time.NewTicker(1 * time.Second)
	for {
		<-ticker.C
		durations := e.services.dialoger.Cleanup()
		e.cleanupRegisterTracker()
		e.cleanupInviteTracker()
		e.cleanupOptionsTracker()
		s := e.services.dialoger.Size()

		for _, d := range durations {
			e.services.metricser.SessionCompleted()
			e.services.metricser.UpdateSPD(d)
		}

		zap.L().Debug("update metrics", zap.Int("size dialogs", s), zap.Int("expired", len(durations)))

		e.services.metricser.UpdateSession(s)
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
	e.initialized.Store(false)
	if e.collection != nil {
		e.collection.Close()
	}
	if e.sock != 0 {
		_ = unix.Close(e.sock) // nolint:gosec // cleanup code, error can be ignored
	}
}

func (e *exporter) IsAlive() bool {
	return e.initialized.Load()
}

func (e *exporter) readPackets() {
	for packet := range e.messages {
		if err := e.parseRawPacket(packet); err != nil {
			e.services.metricser.SystemError()
			zap.L().Error("parse err", zap.Error(err))
		}
	}
}

func (e *exporter) readSocket() {
	buf := make([]byte, readBufSize)

	for {
		n, err := unix.Read(e.sock, buf)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			zap.L().Error("socket read error", zap.Error(err))
			e.services.metricser.SystemError()
			continue
		}

		if n == 0 {
			continue
		}

		packet := make([]byte, n)
		copy(packet, buf[:n])

		zap.L().Debug("packet from socket", zap.Int("len", n))

		e.messages <- packet
	}
}

// parseRawPacket parses raw L2 packet.
func (e *exporter) parseRawPacket(packet []byte) error {
	if len(packet) < 42 {
		return fmt.Errorf("wrong len packet %d", len(packet))
	}

	// Parse Ethernet header (14 bytes)
	ethType := packet[12:14]
	ipOffset := 14

	// VLAN (802.1Q)
	if ethType[0] == 0x81 && ethType[1] == 0x00 {
		if len(packet) < 18 {
			return fmt.Errorf("wrong len packet %d", len(packet))
		}
		ethType = packet[16:18]
		ipOffset = 18
	}

	// Only IPv4
	if ethType[0] != 0x08 || ethType[1] != 0x00 {
		return errors.New("not IPv4 packet")
	}

	// IP header
	if len(packet) < ipOffset+20 {
		return errors.New("ip header too short")
	}

	ipHeader := packet[ipOffset : ipOffset+20]
	ihl := ipHeader[0] & 0x0F
	ipHeaderLen := int(ihl) * 4

	if ipHeader[9] != 17 { // UDP
		return errors.New("not UDP packet")
	}

	// UDP header (8 bytes)
	udpOffset := ipOffset + ipHeaderLen
	if len(packet) < udpOffset+8 {
		return errors.New("udp header too short")
	}

	// SIP data after UDP header
	sipOffset := udpOffset + 8
	if sipOffset >= len(packet) {
		return errors.New("no SIP payload")
	}

	sipData := packet[sipOffset:]

	// Minimum SIP packet should be at least 50 bytes
	if len(sipData) < 50 {
		return fmt.Errorf("packet too small for SIP: %d", len(sipData))
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
		return errors.New("not a SIP packet")
	}

	zap.L().Debug("packet raw", zap.ByteString("sip_data", sipData))

	if err := e.handleMessage(sipData); err != nil {
		return err
	}

	return nil
}

func (e *exporter) sipPacketParse(raw []byte) (dto.Packet, error) {
	lines := bytes.Split(raw, []byte("\r\n"))
	if len(lines) == 0 {
		return dto.Packet{}, fmt.Errorf("split return empty result, raw: %b", raw)
	}

	p := dto.Packet{}
	if bytes.HasPrefix(lines[0], []byte("SIP/2.0")) {
		p.IsResponse = true
		p.ResponseStatus = bytes.TrimPrefix(lines[0], []byte("SIP/2.0 "))[:3]
	} else {
		parts := bytes.SplitN(lines[0], []byte(" "), 3)
		if len(parts) >= 2 {
			p.IsResponse = false
			p.Method = bytes.TrimSpace(parts[0])
		}
	}

	for i, line := range lines {
		if i == 0 {
			continue
		}

		header, value := splitHeader(line)

		switch {
		case bytes.Equal(header, []byte("From")):
			tag := extractTag(value)
			if tag == nil {
				return dto.Packet{}, fmt.Errorf("fail extract tag from '%b'", value)
			}

			p.From.Tag = tag
		case bytes.Equal(header, []byte("To")):
			p.To.Tag = extractTag(value)
		case bytes.Equal(header, []byte("Call-ID")):
			p.CallID = value
		case bytes.Equal(header, []byte("CSeq")):
			id, method := extractCSeq(value)
			if id == nil || method == nil {
				return dto.Packet{}, fmt.Errorf("fail extract CSeq from '%b'", value)
			}

			p.CSeq.Method = method
			p.CSeq.ID = id
		case bytes.Equal(header, []byte("Session-Expires")):
			p.SessionExpires = extractSessionExpires(value)
		}
	}

	return p, nil
}

func (e *exporter) handleMessage(rawPacket []byte) error {
	packet, err := e.sipPacketParse(rawPacket)
	if err != nil {
		return fmt.Errorf("parse SIP packet: %w", err)
	}

	zap.L().Debug("parsed packet", zap.Any("packet", packet))

	if packet.IsResponse {
		e.handleResponse(packet)
	} else {
		e.services.metricser.Request(packet.Method)

		if bytes.Equal(packet.Method, []byte("REGISTER")) {
			e.storeRegisterTime(string(packet.CallID))
		}

		if bytes.Equal(packet.Method, []byte("INVITE")) {
			e.storeInviteTime(string(packet.CallID))
		}

		if bytes.Equal(packet.Method, []byte("OPTIONS")) {
			e.storeOptionsTime(string(packet.CallID))
		}
	}

	return nil
}

func (e *exporter) handleResponse(packet dto.Packet) {
	isInviteResponse := bytes.Equal(packet.CSeq.Method, []byte("INVITE"))
	isRegisterResponse := bytes.Equal(packet.CSeq.Method, []byte("REGISTER"))
	is200OK := bytes.Equal(packet.ResponseStatus, []byte("200"))

	if isInviteResponse && len(packet.ResponseStatus) > 0 {
		if packet.ResponseStatus[0] == '1' {
			if delayMs, ok := e.measureInviteTTR(string(packet.CallID)); ok {
				e.services.metricser.UpdateTTR(delayMs)
			}
		} else {
			e.removeInviteTime(string(packet.CallID))
		}
	}

	isOptionsResponse := bytes.Equal(packet.CSeq.Method, []byte("OPTIONS"))
	if isOptionsResponse {
		if delayMs, ok := e.measureOptionsTime(string(packet.CallID)); ok {
			e.services.metricser.UpdateORD(delayMs)
		}
	}

	e.services.metricser.ResponseWithMetrics(
		packet.ResponseStatus,
		isInviteResponse,
		is200OK,
	)

	if is200OK {
		e.handle200OKResponse(packet, isRegisterResponse)
	} else if isRegisterResponse {
		e.handleRegisterNon200Response(packet)
	}
}

func (e *exporter) handleRegisterNon200Response(packet dto.Packet) {
	if len(packet.ResponseStatus) > 0 && packet.ResponseStatus[0] == '3' {
		if startTime, ok := e.getRegisterTime(string(packet.CallID)); ok {
			delayMs := float64(time.Since(startTime).Nanoseconds()) / 1e6
			e.services.metricser.UpdateLRD(delayMs)
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

func (e *exporter) handle200OKResponse(packet dto.Packet, isRegisterResponse bool) {
	zap.L().Debug("handle message", zap.ByteString("200 OK cseq method", packet.CSeq.Method))

	if bytes.Equal(packet.CSeq.Method, []byte("INVITE")) {
		if err := e.handleInvite200OK(packet); err != nil {
			zap.L().Error("handle INVITE 200 OK", zap.Error(err))
		}
	}

	if bytes.Equal(packet.CSeq.Method, []byte("BYE")) {
		if err := e.handleBye200OK(packet); err != nil {
			zap.L().Error("handle BYE 200 OK", zap.Error(err))
		}
	}

	if isRegisterResponse {
		e.handleRegister200OK(packet)
	}
}

func (e *exporter) handleInvite200OK(packet dto.Packet) error {
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
	e.services.dialoger.Create(dialogID, expiresAt, time.Now())
	return nil
}

func (e *exporter) handleBye200OK(packet dto.Packet) error {
	dialogID, err := normalizeDialogID(packet.CallID, packet.From.Tag, packet.To.Tag)
	if err != nil {
		return fmt.Errorf("normalize dialog ID: %w", err)
	}

	zap.L().Debug("delete sip dialog", zap.String("delete session", dialogID))
	duration := e.services.dialoger.Delete(dialogID)
	if duration > 0 {
		e.services.metricser.UpdateSPD(duration)
		e.services.metricser.SessionCompleted()
	}
	return nil
}

func (e *exporter) handleRegister200OK(packet dto.Packet) {
	startTime, ok := e.getRegisterTime(string(packet.CallID))
	if !ok {
		return
	}

	delayMs := float64(time.Since(startTime).Nanoseconds()) / 1e6
	e.services.metricser.UpdateRRD(delayMs)
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

	start := tagIdx + 5
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
	return (i<<8)&0xFF00 | i>>8
}

func extractCSeq(value []byte) ([]byte, []byte) {
	arr := bytes.Split(value, []byte(" "))
	if len(arr) < 2 {
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

func (e *exporter) storeRegisterTime(callID string) {
	e.registerMutex.Lock()
	defer e.registerMutex.Unlock()
	e.registerTracker[callID] = registerEntry{
		timestamp: time.Now(),
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

func (e *exporter) storeInviteTime(callID string) {
	e.inviteMutex.Lock()
	defer e.inviteMutex.Unlock()
	e.inviteTracker[callID] = inviteEntry{
		timestamp: time.Now(),
	}
}

func (e *exporter) measureInviteTTR(callID string) (float64, bool) {
	e.inviteMutex.Lock()
	defer e.inviteMutex.Unlock()

	entry, ok := e.inviteTracker[callID]
	if !ok {
		return 0, false
	}

	delete(e.inviteTracker, callID)
	delayMs := float64(time.Since(entry.timestamp).Nanoseconds()) / 1e6

	zap.L().Debug("TTR measured",
		zap.String("call_id", callID),
		zap.Float64("delay_ms", delayMs))

	return delayMs, true
}

func (e *exporter) removeInviteTime(callID string) {
	e.inviteMutex.Lock()
	defer e.inviteMutex.Unlock()
	delete(e.inviteTracker, callID)
}

func (e *exporter) storeOptionsTime(callID string) {
	e.optionsMutex.Lock()
	defer e.optionsMutex.Unlock()
	e.optionsTracker[callID] = optionsEntry{
		timestamp: time.Now(),
	}
}

func (e *exporter) measureOptionsTime(callID string) (float64, bool) {
	e.optionsMutex.Lock()
	defer e.optionsMutex.Unlock()

	entry, ok := e.optionsTracker[callID]
	if !ok {
		return 0, false
	}

	delete(e.optionsTracker, callID)
	delayMs := float64(time.Since(entry.timestamp).Nanoseconds()) / 1e6

	zap.L().Debug("ORD measured",
		zap.String("call_id", callID),
		zap.Float64("delay_ms", delayMs))

	return delayMs, true
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
