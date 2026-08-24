package zk

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// Client represents an active or configured connection to a ZKTeco machine.
type Client struct {
	mu        sync.Mutex
	ip        string
	port      int
	timeout   time.Duration
	password  int
	forceUDP  bool
	omitPing  bool
	verbose   bool
	conn      net.Conn
	sessionID uint16
	replyID   uint16
	isConnect bool
	tcp       bool

	userPacketSize int
	nextUID        uint16
	nextUserID     string

	// Cached capacities
	Sizes Sizes
}

// Option configures a ZK client instance.
type Option func(*Client)

// WithPort sets the port number (default 4370).
func WithPort(port int) Option {
	return func(c *Client) {
		c.port = port
	}
}

// WithTimeout sets network socket read/write timeouts (default 10s).
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.timeout = timeout
	}
}

// WithPassword sets commkey communication password (default 0).
func WithPassword(password int) Option {
	return func(c *Client) {
		c.password = password
	}
}

// WithForceUDP forces UDP transport instead of attempting TCP.
func WithForceUDP(forceUDP bool) Option {
	return func(c *Client) {
		c.forceUDP = forceUDP
	}
}

// WithOmitPing skips the ICMP ping check before connecting.
func WithOmitPing(omitPing bool) Option {
	return func(c *Client) {
		c.omitPing = omitPing
	}
}

// WithVerbose enables detailed packet debug logging.
func WithVerbose(verbose bool) Option {
	return func(c *Client) {
		c.verbose = verbose
	}
}

// New creates a new ZKTeco client instance with default options.
func New(ip string, opts ...Option) *Client {
	c := &Client{
		ip:             ip,
		port:           4370,
		timeout:        10 * time.Second,
		password:       0,
		forceUDP:       false,
		omitPing:       false,
		verbose:        false,
		userPacketSize: 72, // default ZK8
		nextUID:        1,
		nextUserID:     "1",
		replyID:        USHRT_MAX - 1,
	}

	for _, opt := range opts {
		opt(c)
	}

	c.tcp = !c.forceUDP
	return c
}

// Ping checks if the device is reachable via system ICMP ping.
func (c *Client) Ping() bool {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("ping", "-n", "1", "-w", "2000", c.ip)
	} else {
		cmd = exec.Command("ping", "-c", "1", "-W", "2", c.ip)
	}
	return cmd.Run() == nil
}

// TestTCP tests if the TCP port is open and responsive.
func (c *Client) TestTCP() bool {
	addr := fmt.Sprintf("%s:%d", c.ip, c.port)
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Connect establishes connection and authenticates with the machine.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.omitPing && !c.Ping() {
		return fmt.Errorf("%w: ping to %s failed", ErrDeviceUnreachable, c.ip)
	}

	addr := fmt.Sprintf("%s:%d", c.ip, c.port)

	if !c.forceUDP && c.TestTCP() {
		c.tcp = true
		c.userPacketSize = 72
		conn, err := net.DialTimeout("tcp", addr, c.timeout)
		if err != nil {
			return &NetworkError{Op: "dial tcp", Err: err}
		}
		c.conn = conn
	} else {
		c.tcp = false
		conn, err := net.DialTimeout("udp", addr, c.timeout)
		if err != nil {
			return &NetworkError{Op: "dial udp", Err: err}
		}
		c.conn = conn
	}

	c.sessionID = 0
	c.replyID = USHRT_MAX - 1

	// Send Connect command
	respCode, data, err := c.rawSendCommand(CmdConnect, nil, 8)
	if err != nil {
		_ = c.conn.Close()
		return err
	}

	if respCode == CmdAckUnauth {
		if c.verbose {
			log.Printf("[zk] Auth required, sending commkey for password=%d, sessionID=%d", c.password, c.sessionID)
		}
		commKey := MakeCommKey(c.password, c.sessionID, 50)
		respCode, data, err = c.rawSendCommand(CmdAuth, commKey, 8)
		if err != nil {
			_ = c.conn.Close()
			return err
		}
	}

	if respCode != CmdAckOk {
		_ = c.conn.Close()
		return fmt.Errorf("%w (response code: %d, data: %x)", ErrUnauthorized, respCode, data)
	}

	c.isConnect = true
	if c.verbose {
		log.Printf("[zk] Successfully connected to %s (sessionID=%d, transport=%s)", addr, c.sessionID, c.transportName())
	}
	return nil
}

// Disconnect gracefully disconnects from the device.
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isConnect || c.conn == nil {
		return nil
	}

	_, _, _ = c.rawSendCommand(CmdExit, nil, 8)
	err := c.conn.Close()
	c.isConnect = false
	c.conn = nil
	return err
}

// IsConnected returns whether client is currently connected.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isConnect
}

func (c *Client) transportName() string {
	if c.tcp {
		return "TCP"
	}
	return "UDP"
}

// rawSendCommand transmits a command and receives a response without acquiring c.mu lock.
func (c *Client) rawSendCommand(cmd uint16, cmdString []byte, expectedRespSize int) (uint16, []byte, error) {
	if cmd != CmdConnect && cmd != CmdAuth && !c.isConnect {
		return 0, nil, ErrNotConnected
	}

	buf, nextReplyID := CreateHeader(cmd, cmdString, c.sessionID, c.replyID)
	c.replyID = nextReplyID

	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))

	if c.tcp {
		top := CreateTCPTop(buf)
		if _, err := c.conn.Write(top); err != nil {
			return 0, nil, &NetworkError{Op: "write tcp", Err: err}
		}

		// Read 8-byte TCP top header
		tcpTopHeader := make([]byte, 8)
		if _, err := io.ReadFull(c.conn, tcpTopHeader); err != nil {
			return 0, nil, &NetworkError{Op: "read tcp top", Err: err}
		}

		payloadLen, err := TestTCPTop(tcpTopHeader)
		if err != nil {
			return 0, nil, err
		}

		// Read payloadLen bytes
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(c.conn, payload); err != nil {
			return 0, nil, &NetworkError{Op: "read tcp payload", Err: err}
		}

		if len(payload) < 8 {
			return 0, nil, fmt.Errorf("%w: response payload shorter than 8 bytes", ErrInvalidTcpHeader)
		}

		respCode := binary.LittleEndian.Uint16(payload[0:2])
		respSessionID := binary.LittleEndian.Uint16(payload[4:6])
		respReplyID := binary.LittleEndian.Uint16(payload[6:8])

		if cmd == CmdConnect {
			c.sessionID = respSessionID
		}
		c.replyID = respReplyID

		data := payload[8:]
		return respCode, data, nil
	}

	// UDP Transport
	if _, err := c.conn.Write(buf); err != nil {
		return 0, nil, &NetworkError{Op: "write udp", Err: err}
	}

	recvBuf := make([]byte, expectedRespSize+8+1024)
	n, err := c.conn.Read(recvBuf)
	if err != nil {
		return 0, nil, &NetworkError{Op: "read udp", Err: err}
	}
	if n < 8 {
		return 0, nil, fmt.Errorf("zk: UDP response packet too short (%d bytes)", n)
	}

	respCode := binary.LittleEndian.Uint16(recvBuf[0:2])
	respSessionID := binary.LittleEndian.Uint16(recvBuf[4:6])
	respReplyID := binary.LittleEndian.Uint16(recvBuf[6:8])

	if cmd == CmdConnect {
		c.sessionID = respSessionID
	}
	c.replyID = respReplyID

	data := recvBuf[8:n]
	return respCode, data, nil
}

// sendCommand transmits a command with thread safety.
func (c *Client) sendCommand(cmd uint16, cmdString []byte, expectedRespSize int) (uint16, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rawSendCommand(cmd, cmdString, expectedRespSize)
}

// ackOK sends an ACK_OK acknowledgment packet.
func (c *Client) ackOK() error {
	buf, _ := CreateHeader(CmdAckOk, nil, c.sessionID, USHRT_MAX-1)
	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))

	if c.tcp {
		top := CreateTCPTop(buf)
		_, err := c.conn.Write(top)
		return err
	}
	_, err := c.conn.Write(buf)
	return err
}
