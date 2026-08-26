package zk

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// LiveCapture starts listening for real-time punch/attendance events and streams them over a Go channel.
// It uses connMu to serialize conn access so that concurrent API calls (SyncAll, DisableDevice, etc.)
// do not race with the live read loop's SetReadDeadline / Read.
func (c *Client) LiveCapture(ctx context.Context) (<-chan Attendance, <-chan error) {
	out := make(chan Attendance, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errChan)

		// Cache users for ID mapping
		users, _ := c.GetUsers()
		uidMap := make(map[string]int)
		for _, u := range users {
			uidMap[u.UserID] = int(u.UID)
		}

		_ = c.CancelCapture()
		_ = c.VerifyUser()
		_ = c.EnableDevice()

		// Register for attendance events
		if err := c.RegEvent(EfAttLog); err != nil {
			errChan <- fmt.Errorf("zk: failed to register event: %w", err)
			return
		}
		defer func() {
			_ = c.RegEvent(0)
		}()

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Check conn still valid
			c.mu.Lock()
			connNil := c.conn == nil
			isTCP := c.tcp
			c.mu.Unlock()
			if connNil {
				return
			}

			// Set read deadline for periodic context checks — must hold connMu.
			c.connMu.Lock()
			if c.conn != nil {
				_ = c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			}
			c.connMu.Unlock()

			var header [4]uint16
			var data []byte
			var readErr error

			if isTCP {
				// TCP: read 8-byte top + payload — hold connMu for the pair.
				c.connMu.Lock()
				tcpHeader := make([]byte, 8)
				if c.conn == nil {
					c.connMu.Unlock()
					return
				}
				_, readErr = io.ReadFull(c.conn, tcpHeader)
				if readErr != nil {
					c.connMu.Unlock()
					var netErr net.Error
					if errors.As(readErr, &netErr) && netErr.Timeout() {
						select {
						case <-ctx.Done():
							return
						default:
						}
						continue
					}
					if errors.Is(readErr, io.EOF) || errors.Is(readErr, net.ErrClosed) {
						return
					}
					continue
				}

				payloadLen, err := TestTCPTop(tcpHeader)
				if err != nil || payloadLen < 8 {
					c.connMu.Unlock()
					continue
				}

				payload := make([]byte, payloadLen)
				if _, err := io.ReadFull(c.conn, payload); err != nil {
					c.connMu.Unlock()
					continue
				}
				c.connMu.Unlock()

				_ = binary.Read(bytes.NewReader(payload[:8]), binary.LittleEndian, &header)
				data = payload[8:]
			} else {
				c.connMu.Lock()
				if c.conn == nil {
					c.connMu.Unlock()
					return
				}
				udpBuf := make([]byte, 1024+8)
				n, err := c.conn.Read(udpBuf)
				c.connMu.Unlock()
				if err != nil {
					var netErr net.Error
					if errors.As(err, &netErr) && netErr.Timeout() {
						select {
						case <-ctx.Done():
							return
						default:
						}
						continue
					}
					if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
						return
					}
					continue
				}
				if n < 8 {
					continue
				}

				_ = binary.Read(bytes.NewReader(udpBuf[:8]), binary.LittleEndian, &header)
				data = udpBuf[8:n]
			}

			if header[0] != CmdRegEvent || len(data) == 0 {
				continue
			}

			// Acknowledge receipt (thread-safe, holds both mu+connMu internally)
			_ = c.ackOK()

			for len(data) >= 10 {
				var userID string
				var uid int
				var status int
				var punch int
				var timeHex []byte

				switch {
				case len(data) == 10:
					uidRaw := binary.LittleEndian.Uint16(data[0:2])
					uid = int(uidRaw)
					status = int(data[2])
					punch = int(data[3])
					timeHex = data[4:10]
					userID = strconv.Itoa(uid)
					data = data[10:]

				case len(data) == 12:
					userIDNum := binary.LittleEndian.Uint32(data[0:4])
					userID = strconv.Itoa(int(userIDNum))
					status = int(data[4])
					punch = int(data[5])
					timeHex = data[6:12]
					uid = uidMap[userID]
					if uid == 0 {
						uid = int(userIDNum)
					}
					data = data[12:]

				case len(data) == 14:
					uidRaw := binary.LittleEndian.Uint16(data[0:2])
					uid = int(uidRaw)
					status = int(data[2])
					punch = int(data[3])
					timeHex = data[4:10]
					userID = strconv.Itoa(uid)
					data = data[14:]

				case len(data) >= 32:
					userID = CleanCString(data[0:24])
					status = int(data[24])
					punch = int(data[25])
					timeHex = data[26:32]
					uid = uidMap[userID]
					if uid == 0 {
						num, _ := strconv.Atoi(userID)
						uid = num
					}

					// Modern TFT devices report fixed stride per firmware; compute from header length
					// when available, fallback to data-length heuristics for legacy devices.
					stride := 32
					if len(data) == 36 {
						stride = 36
					} else if len(data) == 37 {
						stride = 37
					} else if len(data) >= 52 {
						stride = 52
					}
					if stride > len(data) {
						stride = len(data)
					}
					data = data[stride:]

				default:
					data = nil
				}

				if len(timeHex) == 6 {
					ts, err := DecodeTimeHex(timeHex)
					if err == nil {
						att := Attendance{
							UID: uid, UserID: userID, Timestamp: ts, Status: status, Punch: punch,
						}
						select {
						case out <- att:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}
	}()

	return out, errChan
}

// LiveCaptureFunc is a callback-based helper for LiveCapture.
func (c *Client) LiveCaptureFunc(ctx context.Context, callback func(Attendance)) error {
	events, errs := c.LiveCapture(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err, ok := <-errs:
			if ok && err != nil {
				return err
			}
		case event, ok := <-events:
			if !ok {
				return nil
			}
			callback(event)
		}
	}
}

// CancelCapture cancels fingerprint capture mode.
func (c *Client) CancelCapture() error {
	respCode, _, err := c.sendCommand(CmdCancelCapture, nil, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot cancel capture", respCode)
	}
	return nil
}

// VerifyUser switches machine back to standard biometric verification state.
func (c *Client) VerifyUser() error {
	respCode, _, err := c.sendCommand(CmdStartVerify, nil, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot start verify", respCode)
	}
	return nil
}

// RegEvent registers real-time event flags on the machine.
func (c *Client) RegEvent(flags uint32) error {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, flags)

	respCode, _, err := c.sendCommand(CmdRegEvent, buf, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError(fmt.Sprintf("cannot register event flag 0x%08X", flags), respCode)
	}
	return nil
}
