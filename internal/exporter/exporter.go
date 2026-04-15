package exporter

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"gitlab.com/sip-exporter/internal/dto"
	"gitlab.com/sip-exporter/internal/service"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

var (
	ErrUserNotRoot = errors.New("this program requires root privileges")
)

const (
	ethPAll     = 0x0003
	readBufSize = 65536
)

type (
	exporter struct {
		collection *ebpf.Collection
		sock       int
		messages   chan []byte
		services   services
	}
	services struct {
		metricser service.Metricser
		dialoger  service.Dialoger
	}
	Exporter interface {
		Initialize(interfaceName string, path string, sipPort, sipsPort int) error
		Close()
	}
)

func NewExporter(m service.Metricser, d service.Dialoger) Exporter {
	return &exporter{
		services: services{
			metricser: m,
			dialoger:  d,
		},
		messages: make(chan []byte, 10_000),
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

	return nil
}

func (e *exporter) sipDialogMetricsUpdate() {
	ticker := time.NewTicker(1 * time.Second)
	for {
		<-ticker.C
		e.services.dialoger.Cleanup()
		s := e.services.dialoger.Size()

		zap.L().Debug("update metrics", zap.Int("size dialogs", s))

		e.services.metricser.UpdateSession(s)
	}
}

func (e *exporter) Close() {
	if e.collection != nil {
		e.collection.Close()
	}
	if e.sock != 0 {
		_ = unix.Close(e.sock) // nolint:gosec // cleanup code, error can be ignored
	}
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

		// Copy data to avoid races
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
		isInviteResponse := bytes.Equal(packet.CSeq.Method, []byte("INVITE"))
		is200OK := bytes.Equal(packet.ResponseStatus, []byte("200"))

		go func() {
			e.services.metricser.Response(packet.ResponseStatus, isInviteResponse)
			if is200OK && isInviteResponse {
				e.services.metricser.Invite200OK()
			}
		}()

		if is200OK {
			zap.L().Debug("handle message", zap.ByteString("200 OK cseq method", packet.CSeq.Method))

			if bytes.Equal(packet.CSeq.Method, []byte("INVITE")) {
				var dialogID string
				dialogID, err = normalizeDialogID(
					packet.CallID, packet.From.Tag, packet.To.Tag,
				)
				if err != nil {
					return fmt.Errorf("normalize dialog ID: %w", err)
				}

				expires := packet.SessionExpires
				if expires == 0 {
					expires = 1800 // default 30 min
				}
				expiresAt := time.Now().Add(time.Duration(expires) * time.Second)

				zap.L().Debug("create sip dialog",
					zap.String("session", dialogID),
					zap.Int("expires_sec", expires))
				e.services.dialoger.Create(dialogID, expiresAt)
			}

			if bytes.Equal(packet.CSeq.Method, []byte("BYE")) {
				var dialogID string
				dialogID, err = normalizeDialogID(packet.CallID, packet.From.Tag, packet.To.Tag)
				if err != nil {
					return fmt.Errorf("normalize dialog ID: %w", err)
				}

				zap.L().Debug("delete sip dialog", zap.String("delete session", dialogID))

				e.services.dialoger.Delete(dialogID)
				e.services.metricser.SessionCompleted()
			}
		}
	} else {
		go e.services.metricser.Request(packet.Method)
	}

	return nil
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
