package zk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"
)

// CreateChecksum calculates the 16-bit 1's complement checksum for ZKTeco packets.
func CreateChecksum(p []byte) uint16 {
	var checksum int32 = 0
	l := len(p)
	i := 0
	for l > 1 {
		val := int32(binary.LittleEndian.Uint16(p[i : i+2]))
		checksum += val
		i += 2
		l -= 2
		if checksum > int32(USHRT_MAX) {
			checksum -= int32(USHRT_MAX)
		}
	}
	if l > 0 {
		checksum += int32(p[i])
	}
	for checksum > int32(USHRT_MAX) {
		checksum -= int32(USHRT_MAX)
	}
	checksum = ^checksum
	for checksum < 0 {
		checksum += int32(USHRT_MAX)
	}
	return uint16(checksum)
}

// MakeCommKey scrambles password and session_id for authentication (equivalent to commpro.c MakeKey).
func MakeCommKey(key int, sessionID uint16, ticks byte) []byte {
	var k uint32 = 0
	for i := 0; i < 32; i++ {
		if (key & (1 << i)) != 0 {
			k = (k << 1) | 1
		} else {
			k = k << 1
		}
	}
	k += uint32(sessionID)

	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, k)

	buf[0] ^= 'Z'
	buf[1] ^= 'K'
	buf[2] ^= 'S'
	buf[3] ^= 'O'

	// Swap 16-bit words (little endian)
	h0 := binary.LittleEndian.Uint16(buf[0:2])
	h1 := binary.LittleEndian.Uint16(buf[2:4])
	binary.LittleEndian.PutUint16(buf[0:2], h1)
	binary.LittleEndian.PutUint16(buf[2:4], h0)

	b := ticks
	buf[0] ^= b
	buf[1] ^= b
	buf[2] = b
	buf[3] ^= b

	return buf
}

// CreateHeader builds a full packet header with checksum and increments the replyID.
func CreateHeader(command uint16, commandString []byte, sessionID uint16, replyID uint16) ([]byte, uint16) {
	// Step 1: Create dummy packet with 0 checksum to compute real checksum
	rawBuf := new(bytes.Buffer)
	_ = binary.Write(rawBuf, binary.LittleEndian, command)
	_ = binary.Write(rawBuf, binary.LittleEndian, uint16(0))
	_ = binary.Write(rawBuf, binary.LittleEndian, sessionID)
	_ = binary.Write(rawBuf, binary.LittleEndian, replyID)
	rawBuf.Write(commandString)

	checksum := CreateChecksum(rawBuf.Bytes())

	replyID++
	if replyID >= USHRT_MAX {
		replyID -= USHRT_MAX
	}

	// Step 2: Build actual header with calculated checksum
	packetBuf := new(bytes.Buffer)
	_ = binary.Write(packetBuf, binary.LittleEndian, command)
	_ = binary.Write(packetBuf, binary.LittleEndian, checksum)
	_ = binary.Write(packetBuf, binary.LittleEndian, sessionID)
	_ = binary.Write(packetBuf, binary.LittleEndian, replyID)
	packetBuf.Write(commandString)

	return packetBuf.Bytes(), replyID
}

// CreateTCPTop wraps a packet with the 8-byte TCP header prefix (0x5050 0x7D82 <len>).
func CreateTCPTop(packet []byte) []byte {
	length := uint32(len(packet))
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, MachinePrepareData1)
	_ = binary.Write(buf, binary.LittleEndian, MachinePrepareData2)
	_ = binary.Write(buf, binary.LittleEndian, length)
	buf.Write(packet)
	return buf.Bytes()
}

// TestTCPTop checks and extracts the packet length from the 8-byte TCP top header.
func TestTCPTop(packet []byte) (int, error) {
	if len(packet) < 8 {
		return 0, fmt.Errorf("%w: packet shorter than 8 bytes (len=%d)", ErrInvalidTcpHeader, len(packet))
	}
	m1 := binary.LittleEndian.Uint16(packet[0:2])
	m2 := binary.LittleEndian.Uint16(packet[2:4])
	if m1 != MachinePrepareData1 || m2 != MachinePrepareData2 {
		return 0, fmt.Errorf("%w: magic bytes mismatch (0x%04X 0x%04X)", ErrInvalidTcpHeader, m1, m2)
	}
	length := binary.LittleEndian.Uint32(packet[4:8])
	return int(length), nil
}

// EncodeTime encodes a standard time.Time into a 32-bit unsigned integer time format used by ZK.
func EncodeTime(t time.Time) uint32 {
	year := t.Year() % 100
	month := int(t.Month()) - 1
	day := t.Day() - 1
	hour := t.Hour()
	minute := t.Minute()
	second := t.Second()

	days := (year*12*31 + month*31 + day) * 86400
	secs := (hour*60+minute)*60 + second
	return uint32(days + secs)
}

// DecodeTime decodes a 32-bit unsigned integer time format from ZK into time.Time.
func DecodeTime(raw uint32) time.Time {
	sec := int(raw % 60)
	raw /= 60
	min := int(raw % 60)
	raw /= 60
	hour := int(raw % 24)
	raw /= 24
	day := int(raw%31) + 1
	raw /= 31
	month := time.Month((raw % 12) + 1)
	raw /= 12
	year := int(raw) + 2000

	return time.Date(year, month, day, hour, min, sec, 0, time.Local)
}

// DecodeTimeBytes unpacks 4 bytes little endian and decodes to time.Time.
func DecodeTimeBytes(b []byte) time.Time {
	if len(b) < 4 {
		return time.Time{}
	}
	raw := binary.LittleEndian.Uint32(b[:4])
	return DecodeTime(raw)
}

// DecodeTimeHex decodes 6-byte datetime format [year, month, day, hour, minute, second].
func DecodeTimeHex(b []byte) (time.Time, error) {
	if len(b) < 6 {
		return time.Time{}, fmt.Errorf("zk: timehex requires 6 bytes, got %d", len(b))
	}
	year := int(b[0]) + 2000
	month := time.Month(b[1])
	day := int(b[2])
	hour := int(b[3])
	minute := int(b[4])
	second := int(b[5])

	return time.Date(year, month, day, hour, minute, second, 0, time.Local), nil
}
