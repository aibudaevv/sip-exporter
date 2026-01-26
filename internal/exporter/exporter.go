package exporter

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
	"gitlab.com/sip-exporter/internal/dto"
	"gitlab.com/sip-exporter/internal/service"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
	"net"
	"syscall"
)

var (
	ErrUserNotRoot = errors.New("this program requires root privileges")
)

const ETH_P_ALL = 0x0003

type (
	exporter struct {
		collection *ebpf.Collection
		sock       int
		reader     *ringbuf.Reader
		messages   chan []byte
		services   services
	}
	services struct {
		metricser service.Metricser
		dialoger  service.Dialoger
	}
	Exporter interface {
		Initialize(interfaceName, path string) error
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

func (e *exporter) Initialize(interfaceName, path string) error {
	if syscall.Geteuid() != 0 {
		return ErrUserNotRoot
	}

	collection, err := ebpf.LoadCollection(path)
	if err != nil {
		return fmt.Errorf("failed to load BPF collection: %v", err)
	}

	e.collection = collection

	prog := collection.Programs["bpf_socket_filter"]
	if prog == nil {
		return fmt.Errorf("failed to find BPF program: bpf_socket_filter")
	}

	rbMap := collection.Maps["rb"]
	if rbMap == nil {
		return fmt.Errorf("failed to find ringbuf map: rb")
	}

	reader, err := ringbuf.NewReader(rbMap)
	if err != nil {
		return fmt.Errorf("failed to create ringbuf reader: %v", err)
	}

	e.reader = reader
	sock, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(ETH_P_ALL)))
	if err != nil {
		return fmt.Errorf("failed to create AF_PACKET socket: %v", err)
	}
	e.sock = sock

	ifaceName := interfaceName
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return fmt.Errorf("interface %s not found: %v", ifaceName, err)
	}

	sa := &unix.SockaddrLinklayer{
		Protocol: htons(ETH_P_ALL),
		Ifindex:  iface.Index,
	}

	err = unix.Bind(sock, sa)
	if err != nil {
		return fmt.Errorf("failed to bind AF_PACKET socket to %s: %v", ifaceName, err)
	}

	progFD := prog.FD()
	if err = unix.SetsockoptInt(sock, unix.SOL_SOCKET, unix.SO_ATTACH_BPF, progFD); err != nil {
		return fmt.Errorf("failed to attach BPF program: %v", err)
	}

	zap.L().Info("eBPF program attached to AF_PACKET socket on interface and monitoring SIP traffic...",
		zap.String("interface", interfaceName))

	go e.readPackets()
	go e.readEBPF(reader)

	return nil
}

func (e *exporter) Close() {
	if e.reader != nil {
		e.reader.Close()
	}
	if e.collection != nil {
		e.collection.Close()
	}
	if e.sock != 0 {
		unix.Close(e.sock)
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

func (e *exporter) readEBPF(reader *ringbuf.Reader) {
	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				//FIXME: ???
				zap.L().Info("Ring buffer closed, exiting...")
				return
			}

			e.services.metricser.SystemError()
			zap.L().Error("reading from ringbuf", zap.Error(err))
			continue
		}

		packet := record.RawSample
		if len(packet) == 0 {
			continue
		}

		fmt.Println(string(packet))
		//zap.L().Debug("read packet", zap.ByteString("packet", packet))
		e.messages <- packet
	}
}

// parsing raw L2 packet
func (e *exporter) parseRawPacket(packet []byte) error {
	pktLen := binary.LittleEndian.Uint32(packet[0:4])
	srcPort := binary.LittleEndian.Uint16(packet[4:6])
	dstPort := binary.LittleEndian.Uint16(packet[6:8])

	zap.L().Debug("packet",
		zap.Uint16("source port", srcPort),
		zap.Uint16("destination port", dstPort),
		zap.Uint32("real size", pktLen),
		zap.Int("userspace size", len(packet)))

	packetData := packet[8:]

	if len(packetData) < 14 {
		return fmt.Errorf("no ethernet header")
	}

	// IP header (0x45 it IPv4)
	ipVersionIHL := packetData[14]
	if ipVersionIHL == 0 {
		return fmt.Errorf("invalid IP header: %02x", ipVersionIHL)
	}

	ipIHL := ipVersionIHL & 0x0F
	ipHeaderLen := int(ipIHL) * 4

	sipOffset := 14 + ipHeaderLen + 8 // Eth + IP + UDP
	if len(packetData) < sipOffset {
		return fmt.Errorf("headers too long: %d", sipOffset)
	}

	// SIP message
	sipData := packetData[sipOffset:]
	scanner := bytes.NewReader(sipData)

	var pack []byte
	for {
		b, err := scanner.ReadByte()
		if err != nil {
			break
		}
		if b != '\r' {
			pack = append(pack, b)
		}
	}

	if len(pack) > 0 {
		if err := e.handleMessage(pack); err != nil {
			return err
		}
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
				return dto.Packet{}, fmt.Errorf("fail extact tag from '%b'", value)
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
		}
	}
	return p, nil
}

func (e *exporter) handleMessage(rawPacket []byte) error {
	packet, err := e.sipPacketParse(rawPacket)
	if err != nil {
		return err
	}

	if packet.IsResponse {
		go e.services.metricser.Response(packet.ResponseStatus)
		switch packet.ResponseStatus {
		case []byte("200"):
			if bytes.Equal(packet.CSeq.Method, []byte("INVITE")) {
				dialogID, errd := normalizeDialogID(packet.CallID, packet.From.Tag, packet.To.Tag)
				if errd != nil {
					return err
				}

				e.services.dialoger.Create(dialogID)
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
