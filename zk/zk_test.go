package zk

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"testing"
	"time"
)

func TestCreateChecksum(t *testing.T) {
	tests := []struct {
		input    []byte
		expected uint16
	}{
		{input: []byte{}, expected: 65534},
		{input: []byte("hello"), expected: 11707},
		{input: []byte{1, 2, 3, 4, 5, 6, 7, 8}, expected: 60398},
		{input: []byte("192.168.1.201"), expected: 54685},
	}

	for _, tt := range tests {
		got := CreateChecksum(tt.input)
		if got != tt.expected {
			t.Errorf("CreateChecksum(%q) = %d, expected %d", tt.input, got, tt.expected)
		}
	}
}

func TestMakeCommKey(t *testing.T) {
	tests := []struct {
		key         int
		sessionID   uint16
		expectedHex string
	}{
		{key: 0, sessionID: 1234, expectedHex: "617d327d"},
		{key: 123456, sessionID: 5678, expectedHex: "267f32ef"},
		{key: 999999, sessionID: 1, expectedHex: "23813289"},
	}

	for _, tt := range tests {
		got := MakeCommKey(tt.key, tt.sessionID, 50)
		gotHex := hex.EncodeToString(got)
		if gotHex != tt.expectedHex {
			t.Errorf("MakeCommKey(%d, %d) = %s, expected %s", tt.key, tt.sessionID, gotHex, tt.expectedHex)
		}
	}
}

func TestTimeEncodingDecoding(t *testing.T) {
	sampleTimes := []time.Time{
		time.Date(2026, 8, 24, 12, 10, 30, 0, time.Local),
		time.Date(2023, 1, 1, 0, 0, 0, 0, time.Local),
		time.Date(2025, 12, 31, 23, 59, 59, 0, time.Local),
	}

	for _, original := range sampleTimes {
		encoded := EncodeTime(original)
		decoded := DecodeTime(encoded)

		if !original.Equal(decoded) {
			t.Errorf("Time roundtrip failed: original=%v, encoded=%d, decoded=%v", original, encoded, decoded)
		}
	}
}

func TestTimeHexDecoding(t *testing.T) {
	// [year (since 2000), month, day, hour, min, sec]
	// 2026-08-24 14:30:15 => [26, 8, 24, 14, 30, 15]
	rawHex := []byte{26, 8, 24, 14, 30, 15}
	decoded, err := DecodeTimeHex(rawHex)
	if err != nil {
		t.Fatalf("DecodeTimeHex failed: %v", err)
	}

	expected := time.Date(2026, 8, 24, 14, 30, 15, 0, time.Local)
	if !decoded.Equal(expected) {
		t.Errorf("DecodeTimeHex = %v, expected %v", decoded, expected)
	}
}

func TestTCPTopHeader(t *testing.T) {
	packet := []byte("hello world")
	top := CreateTCPTop(packet)

	if len(top) != len(packet)+8 {
		t.Fatalf("Expected top header length %d, got %d", len(packet)+8, len(top))
	}

	payloadLen, err := TestTCPTop(top)
	if err != nil {
		t.Fatalf("TestTCPTop failed: %v", err)
	}

	if payloadLen != len(packet) {
		t.Errorf("Expected payload length %d, got %d", len(packet), payloadLen)
	}
}

func TestUserRepack(t *testing.T) {
	user := User{
		UID:       10,
		Name:      "Budi Santoso",
		Privilege: UserAdmin,
		Password:  "1234",
		GroupID:   "1",
		UserID:    "1001",
		Card:      12345678,
	}

	if !user.IsEnabled() || user.IsDisabled() {
		t.Error("User should be enabled")
	}

	if user.UserType() != UserAdmin {
		t.Errorf("Expected UserAdmin, got %d", user.UserType())
	}

	repack29 := user.Repack29()
	if len(repack29) != 29 {
		t.Errorf("Repack29 length = %d, expected 29", len(repack29))
	}
	if repack29[0] != 2 {
		t.Errorf("Repack29 prefix = %d, expected 2", repack29[0])
	}

	repack73 := user.Repack73()
	if len(repack73) != 73 {
		t.Errorf("Repack73 length = %d, expected 73", len(repack73))
	}
	if repack73[0] != 2 {
		t.Errorf("Repack73 prefix = %d, expected 2", repack73[0])
	}
}

func TestFingerRepack(t *testing.T) {
	template := bytes.Repeat([]byte{0xAB}, 384)
	finger := Finger{
		UID:      1,
		FID:      0,
		Valid:    1,
		Template: template,
		Size:     len(template),
	}

	repack := finger.Repack()
	if len(repack) != len(template)+6 {
		t.Errorf("Finger Repack len = %d, expected %d", len(repack), len(template)+6)
	}

	repackOnly := finger.RepackOnly()
	if len(repackOnly) != len(template)+2 {
		t.Errorf("Finger RepackOnly len = %d, expected %d", len(repackOnly), len(template)+2)
	}
}

func TestMockTCPServer(t *testing.T) {
	// Setup a local mock TCP server simulating ZKTeco device
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start mock listener: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	// Run mock server in goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				for {
					topHeader := make([]byte, 8)
					if _, err := io.ReadFull(c, topHeader); err != nil {
						return
					}

					payloadLen, err := TestTCPTop(topHeader)
					if err != nil || payloadLen < 8 {
						return
					}

					payload := make([]byte, payloadLen)
					if _, err := io.ReadFull(c, payload); err != nil {
						return
					}

					cmd := binary.LittleEndian.Uint16(payload[0:2])
					sessionID := uint16(1234)
					replyID := binary.LittleEndian.Uint16(payload[6:8])

					var respPayload []byte
					switch cmd {
					case CmdConnect:
						// Respond ACK_OK with session ID
						respPayload, _ = CreateHeader(CmdAckOk, nil, sessionID, replyID)
					case CmdGetVersion:
						// Respond with firmware version string
						respPayload, _ = CreateHeader(CmdAckOk, []byte("Ver 6.60 (test)\x00"), sessionID, replyID)
					case CmdExit:
						respPayload, _ = CreateHeader(CmdAckOk, nil, sessionID, replyID)
					default:
						respPayload, _ = CreateHeader(CmdAckOk, nil, sessionID, replyID)
					}

					top := CreateTCPTop(respPayload)
					if _, err := c.Write(top); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	client := New("127.0.0.1", WithPort(port), WithOmitPing(true), WithTimeout(2*time.Second))
	if err := client.Connect(); err != nil {
		t.Fatalf("Failed to connect to mock server: %v", err)
	}
	defer client.Disconnect()

	if !client.IsConnected() {
		t.Error("Client should report connected")
	}

	fw, err := client.GetFirmwareVersion()
	if err != nil {
		t.Fatalf("Failed to get firmware version: %v", err)
	}
	if fw != "Ver 6.60 (test)" {
		t.Errorf("Expected firmware 'Ver 6.60 (test)', got '%s'", fw)
	}
}
