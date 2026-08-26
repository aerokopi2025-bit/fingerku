package zk

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// Client represents an active or configured connection to a ZKTeco machine.
type Client struct {
	mu        sync.Mutex // protects sessionID, replyID, isConnect, tcp, userPacketSize, nextUID, Sizes
	connMu    sync.Mutex // serializes all conn Read/Write + SetDeadline; LiveCapture and sendCommand share it
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
		ip: ip, port: 4370, timeout: 10 * time.Second, password: 0, forceUDP: false, omitPing: false, verbose: false, userPacketSize: 72, // default ZK8
		nextUID: 1, nextUserID: "1", replyID: USHRT_MAX - 1,
	}

	for _, opt := range opts {
		opt(c)
	}

	c.tcp = !c.forceUDP
	return c
}

// Ping checks if the device is reachable via TCP dial (port probe).
// Replaces the previous exec-based ICMP ping to avoid forking and to work
// in restricted environments. Returns true if the TCP port accepts a connection.
func (c *Client) Ping() bool {
	return c.TestTCP()
}

// TestTCP tests if the TCP port is open and responsive via net.JoinHostPort
// (IPv6-safe).
func (c *Client) TestTCP() bool {
	addr := net.JoinHostPort(c.ip, fmt.Sprintf("%d", c.port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Connect establishes connection and authenticates with the machine.
// Network probes (Ping/TestTCP) are done outside the mutex to avoid blocking
// other callers for 6-14s. Only state mutation is held under lock.
func (c *Client) Connect() error {
	// Single probe outside lock — no double TestTCP.
	if !c.omitPing && !c.forceUDP {
		if !c.TestTCP() {
			return fmt.Errorf("%w: ping to %s failed", ErrDeviceUnreachable, c.ip)
		}
	}

	addr := net.JoinHostPort(c.ip, fmt.Sprintf("%d", c.port))

	var (
		conn   net.Conn
		useTCP bool
		err    error
	)
	if !c.forceUDP {
		// Try TCP once; on failure fall back to UDP instead of probing twice.
		conn, err = net.DialTimeout("tcp", addr, c.timeout)
		if err == nil {
			useTCP = true
		} else if c.omitPing {
			conn, err = net.DialTimeout("udp", addr, c.timeout)
			if err != nil {
				return &NetworkError{Op: "dial udp", Err: err}
			}
			useTCP = false
		} else {
			return &NetworkError{Op: "dial tcp", Err: err}
		}
	} else {
		conn, err = net.DialTimeout("udp", addr, c.timeout)
		if err != nil {
			return &NetworkError{Op: "dial udp", Err: err}
		}
		useTCP = false
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.connMu.Lock()
	defer c.connMu.Unlock()

	c.conn = conn
	c.tcp = useTCP
	if useTCP {
		c.userPacketSize = 72
	}

	c.sessionID = 0
	c.replyID = USHRT_MAX - 1

	// Send Connect command
	respCode, data, err := c.rawSendCommandLocked(CmdConnect, nil, 8)
	if err != nil {
		_ = c.conn.Close()
		return err
	}

	if respCode == CmdAckUnauth {
		if c.verbose {
			log.Printf("[zk] Auth required, sending commkey for password=%d, sessionID=%d", c.password, c.sessionID)
		}
		commKey := MakeCommKey(c.password, c.sessionID, 50)
		respCode, data, err = c.rawSendCommandLocked(CmdAuth, commKey, 8)
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
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if !c.isConnect || c.conn == nil {
		return nil
	}

	_, _, _ = c.rawSendCommandLocked(CmdExit, nil, 8)
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

// rawSendCommand transmits a command and receives a response without acquiring locks.
// Caller must hold both c.mu and c.connMu.
func (c *Client) rawSendCommand(cmd uint16, cmdString []byte, expectedRespSize int) (uint16, []byte, error) {
	return c.rawSendCommandLocked(cmd, cmdString, expectedRespSize)
}

// rawSendCommandLocked is the inner implementation; caller must hold mu+connMu.
func (c *Client) rawSendCommandLocked(cmd uint16, cmdString []byte, expectedRespSize int) (uint16, []byte, error) {
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

// sendCommand transmits a command with thread safety (both mu + connMu).
func (c *Client) sendCommand(cmd uint16, cmdString []byte, expectedRespSize int) (uint16, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connMu.Lock()
	defer c.connMu.Unlock()
	return c.rawSendCommandLocked(cmd, cmdString, expectedRespSize)
}

// ackOK sends an ACK_OK acknowledgment packet in a thread-safe manner.
func (c *Client) ackOK() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connMu.Lock()
	defer c.connMu.Unlock()
	return c.ackOKLocked()
}

// ackOKLocked is the internal version that assumes mu+connMu are already held.
func (c *Client) ackOKLocked() error {
	if c.conn == nil {
		return ErrNotConnected
	}
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

// ClearAttendance removes all attendance logs from device RAM.
func (c *Client) ClearAttendance() error {
	respCode, _, err := c.sendCommand(CmdClearAttLog, nil, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot clear attendance logs", respCode)
	}
	return c.RefreshData()
}
