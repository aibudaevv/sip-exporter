package exporter

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
	constant "gitlab.com/sip-exporter/internal"
	"gitlab.com/sip-exporter/internal/service"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
	"log"
	"net"
	"syscall"
)

var (
	ErrUserNotRoot = errors.New("this program requires root privileges")
)

const ETH_P_ALL = 0x0003

type (
	exporter struct {
		m          service.Metricser
		collection *ebpf.Collection
		sock       int
		reader     *ringbuf.Reader
		messages   chan []byte
	}
	Exporter interface {
		Initialize(interfaceName, path string) error
		Close()
	}
)

func NewExporter(m service.Metricser) Exporter {
	return &exporter{
		m:        m,
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
		if err := e.parse(packet); err != nil {
			e.m.SystemError()
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
				log.Println("Ring buffer closed, exiting...")
				return
			}

			e.m.SystemError()
			zap.L().Error("reading from ringbuf", zap.Error(err))
			continue
		}

		packet := record.RawSample
		if len(packet) == 0 {
			continue
		}

		e.messages <- packet
	}
}

// parsing raw L2 packet
func (e *exporter) parse(packet []byte) error {
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

	var line []byte
	for {
		b, err := scanner.ReadByte()
		if err != nil {
			break
		}
		if b != '\r' {
			line = append(line, b)
		}
	}

	if len(line) > 0 {
		if err := e.handleMessage(line); err != nil {
			return err
		}
	}
	return nil
}

func (e *exporter) handleMessage(line []byte) error {
	methodOrStatus := e.getMethodOrStatus(line)
	if methodOrStatus == nil {
		return fmt.Errorf("method of status is empty in '%s'", string(line))
	}

	go e.m.StatusOrCode(methodOrStatus)

	methodOrStatusStr := string(methodOrStatus)

	switch methodOrStatusStr {
	case constant.Invite:
		if err := e.handleInvite(line); err != nil {
			zap.L().Error("handle invite", zap.Error(err))
			e.m.SystemError()
		}
	case constant.Bye:
	case constant.StatusOK:
	}

	return nil
}

func (e *exporter) handleInvite(line []byte) error {

	return nil
}

func (e *exporter) getMethodOrStatus(firstLine []byte) []byte {
	line := bytes.TrimSpace(firstLine)
	if len(line) == 0 {
		return nil
	}

	parts := bytes.Split(line, []byte{' '})
	if len(parts) >= 3 {
		isRes := isResponse(parts)
		if isRes {
			return getStatus(parts)
		}

		return getMethodName(parts)
	}

	return nil
}

func getMethodName(parts [][]byte) []byte {
	return parts[0]
}

func getStatus(parts [][]byte) []byte {
	return parts[1]
}

func isResponse(parts [][]byte) bool {
	if len(parts) >= 2 && bytes.Equal(parts[0], []byte("SIP/2.0")) {
		return true
	}

	return false
}

func htons(i uint16) uint16 {
	return (i<<8)&0xFF00 | i>>8
}
