package alsaseq

// client.go — open a /dev/snd/seq fd, create a port, subscribe to a source.
// Replaces the 4-step open/CLIENT_ID/CREATE_PORT/SUBSCRIBE sequence that was
// duplicated in five places: push-manager's midi.go (LED/CC output port,
// "Push Manager In"), automation's midi.go (CC output port, clock input
// port), and keyboard-visualizer's midi.go ("Keyboard Viz In").

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"unsafe"
)

// Addr is an ALSA sequencer client:port address.
type Addr struct {
	Client byte
	Port   byte
}

// Client is an open connection to /dev/snd/seq with one created port.
type Client struct {
	fd   int
	id   byte // our ALSA client ID
	port byte // our created port
}

// Open opens /dev/snd/seq and resolves our client ID. The client has no
// port yet — call CreatePort next.
func Open() (*Client, error) {
	fd, err := syscall.Open(Dev, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", Dev, err)
	}

	var clientID int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd),
		IoctlClientID, uintptr(unsafe.Pointer(&clientID))); errno != 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("CLIENT_ID ioctl: %w", errno)
	}

	return &Client{fd: fd, id: byte(clientID)}, nil
}

// CreatePort creates a port named name with the given capability and port
// type bits (see CapRead/CapWrite/CapSubsRead/CapSubsWrite and
// PortTypeMidi/PortTypeApp), and returns the created port number.
func (c *Client) CreatePort(name string, caps, typ uint32) (byte, error) {
	portInfo := make([]byte, PortInfoSize)
	portInfo[PortOffAddrClient] = c.id
	copy(portInfo[PortOffName:], name+"\x00")
	binary.LittleEndian.PutUint32(portInfo[PortOffCapability:], caps)
	binary.LittleEndian.PutUint32(portInfo[PortOffType:], typ)

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(c.fd),
		IoctlCreatePort, uintptr(unsafe.Pointer(&portInfo[0]))); errno != 0 {
		return 0, fmt.Errorf("CREATE_PORT ioctl: %w", errno)
	}
	c.port = portInfo[PortOffAddrPort]
	return c.port, nil
}

// Subscribe subscribes our port to receive events sent from src.
func (c *Client) Subscribe(src Addr) error {
	sub := make([]byte, SubSize)
	sub[SubOffSenderClient] = src.Client
	sub[SubOffSenderPort] = src.Port
	sub[SubOffDestClient] = c.id
	sub[SubOffDestPort] = c.port

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(c.fd),
		IoctlSubscribePort, uintptr(unsafe.Pointer(&sub[0]))); errno != 0 {
		return fmt.Errorf("SUBSCRIBE_PORT ioctl: %w", errno)
	}
	return nil
}

// Addr returns this client's own client:port address.
func (c *Client) Addr() Addr { return Addr{Client: c.id, Port: c.port} }

// FD returns the underlying file descriptor, e.g. for a blocking Read loop.
func (c *Client) FD() int { return c.fd }

// Close closes the underlying fd.
func (c *Client) Close() error { return syscall.Close(c.fd) }
