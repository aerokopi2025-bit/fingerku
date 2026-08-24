package zk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// EnableDevice re-enables normal operation and user interaction on the machine.
func (c *Client) EnableDevice() error {
	respCode, _, err := c.sendCommand(CmdEnableDevice, nil, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot enable device", respCode)
	}
	return nil
}

// DisableDevice disables user interaction and locks the machine (displays 'Working...').
func (c *Client) DisableDevice() error {
	respCode, _, err := c.sendCommand(CmdDisableDevice, nil, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot disable device", respCode)
	}
	return nil
}

// GetFirmwareVersion retrieves the device firmware edition string.
func (c *Client) GetFirmwareVersion() (string, error) {
	respCode, data, err := c.sendCommand(CmdGetVersion, nil, 1024)
	if err != nil {
		return "", err
	}
	if respCode != CmdAckOk {
		return "", NewResponseError("cannot read firmware version", respCode)
	}
	return CleanCString(data), nil
}

// getOptionValue reads a named configuration parameter from the device.
func (c *Client) getOptionValue(key string) (string, error) {
	cmdStr := []byte(key + "\x00")
	respCode, data, err := c.sendCommand(CmdOptionsRrq, cmdStr, 1024)
	if err != nil {
		return "", err
	}
	if respCode != CmdAckOk {
		return "", NewResponseError(fmt.Sprintf("cannot read option '%s'", key), respCode)
	}

	str := string(data)
	if idx := strings.IndexByte(str, 0); idx != -1 {
		str = str[:idx]
	}
	if parts := strings.SplitN(str, "=", 2); len(parts) == 2 {
		return strings.TrimSpace(parts[1]), nil
	}
	return strings.TrimSpace(str), nil
}

// GetSerialNumber reads the unique hardware serial number.
func (c *Client) GetSerialNumber() (string, error) {
	return c.getOptionValue("~SerialNumber")
}

// GetPlatform reads the internal hardware platform architecture.
func (c *Client) GetPlatform() (string, error) {
	return c.getOptionValue("~Platform")
}

// GetMAC reads the device MAC address.
func (c *Client) GetMAC() (string, error) {
	return c.getOptionValue("MAC")
}

// GetDeviceName reads the device model/brand name.
func (c *Client) GetDeviceName() (string, error) {
	val, err := c.getOptionValue("~DeviceName")
	if err != nil {
		return "", nil // fallback empty
	}
	return val, nil
}

// GetFaceVersion returns the facial recognition algorithm version.
func (c *Client) GetFaceVersion() (int, error) {
	val, err := c.getOptionValue("ZKFaceVersion")
	if err != nil {
		return 0, nil
	}
	num, _ := strconv.Atoi(val)
	return num, nil
}

// GetFPVersion returns the fingerprint algorithm version (e.g., 9 or 10).
func (c *Client) GetFPVersion() (int, error) {
	val, err := c.getOptionValue("~ZKFPVersion")
	if err != nil {
		return 0, err
	}
	num, _ := strconv.Atoi(val)
	return num, nil
}

// GetExtendFmt returns extend format config value.
func (c *Client) GetExtendFmt() (int, error) {
	val, err := c.getOptionValue("~ExtendFmt")
	if err != nil {
		return 0, nil
	}
	num, _ := strconv.Atoi(val)
	return num, nil
}

// GetUserExtendFmt returns user extend format config value.
func (c *Client) GetUserExtendFmt() (int, error) {
	val, err := c.getOptionValue("~UserExtFmt")
	if err != nil {
		return 0, nil
	}
	num, _ := strconv.Atoi(val)
	return num, nil
}

// GetFaceFunOn returns whether face recognition feature is enabled.
func (c *Client) GetFaceFunOn() (int, error) {
	val, err := c.getOptionValue("FaceFunOn")
	if err != nil {
		return 0, nil
	}
	num, _ := strconv.Atoi(val)
	return num, nil
}

// GetCompatOldFirmware returns compatibility mode flag.
func (c *Client) GetCompatOldFirmware() (int, error) {
	val, err := c.getOptionValue("CompatOldFirmware")
	if err != nil {
		return 0, nil
	}
	num, _ := strconv.Atoi(val)
	return num, nil
}

// GetNetworkParams reads device network parameters (IP, Subnet Mask, Gateway).
func (c *Client) GetNetworkParams() (*NetworkParams, error) {
	ip, _ := c.getOptionValue("IPAddress")
	if ip == "" {
		ip = c.ip
	}
	mask, _ := c.getOptionValue("NetMask")
	gw, _ := c.getOptionValue("GATEIPAddress")

	return &NetworkParams{
		IP:      ip,
		Mask:    mask,
		Gateway: gw,
	}, nil
}

// GetPinWidth returns the maximum PIN/User ID character length.
func (c *Client) GetPinWidth() (int, error) {
	respCode, data, err := c.sendCommand(CmdGetPinWidth, []byte(" P"), 9)
	if err != nil {
		return 0, err
	}
	if respCode != CmdAckOk || len(data) == 0 {
		return 0, NewResponseError("cannot get pin width", respCode)
	}
	return int(data[0]), nil
}

// ReadSizes queries memory usage and capacity statistics.
func (c *Client) ReadSizes() (*Sizes, error) {
	respCode, data, err := c.sendCommand(CmdGetFreeSizes, nil, 1024)
	if err != nil {
		return nil, err
	}
	if respCode != CmdAckOk {
		return nil, NewResponseError("cannot read memory sizes", respCode)
	}

	sizes := Sizes{}
	if len(data) >= 80 {
		var fields [20]int32
		reader := bytes.NewReader(data[:80])
		_ = binary.Read(reader, binary.LittleEndian, &fields)

		sizes.Users = int(fields[4])
		sizes.Fingers = int(fields[6])
		sizes.Records = int(fields[8])
		sizes.Cards = int(fields[12])
		sizes.FingersCap = int(fields[14])
		sizes.UsersCap = int(fields[15])
		sizes.RecordsCap = int(fields[16])
		sizes.FingersAvailable = int(fields[17])
		sizes.UsersAvailable = int(fields[18])
		sizes.RecordsAvailable = int(fields[19])
	}

	if len(data) >= 92 {
		var faceFields [3]int32
		reader := bytes.NewReader(data[80:92])
		_ = binary.Read(reader, binary.LittleEndian, &faceFields)
		sizes.Faces = int(faceFields[0])
		sizes.FacesCap = int(faceFields[2])
	}

	c.mu.Lock()
	c.Sizes = sizes
	c.mu.Unlock()

	return &sizes, nil
}

// GetTime retrieves the current RTC timestamp from the device.
func (c *Client) GetTime() (time.Time, error) {
	respCode, data, err := c.sendCommand(CmdGetTime, nil, 1032)
	if err != nil {
		return time.Time{}, err
	}
	if respCode != CmdAckOk || len(data) < 4 {
		return time.Time{}, NewResponseError("cannot get machine time", respCode)
	}
	return DecodeTimeBytes(data[:4]), nil
}

// SetTime updates the RTC clock on the device.
func (c *Client) SetTime(t time.Time) error {
	encoded := EncodeTime(t)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, encoded)

	respCode, _, err := c.sendCommand(CmdSetTime, buf, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot set machine time", respCode)
	}
	return nil
}

// Restart reboots the device.
func (c *Client) Restart() error {
	respCode, _, err := c.sendCommand(CmdRestart, nil, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot restart device", respCode)
	}
	_ = c.Disconnect()
	return nil
}

// PowerOff turns off the machine.
func (c *Client) PowerOff() error {
	respCode, _, err := c.sendCommand(CmdPowerOff, nil, 1032)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot power off device", respCode)
	}
	_ = c.Disconnect()
	return nil
}

// Unlock triggers the access control door relay for specified duration (in seconds).
func (c *Client) Unlock(delaySeconds int) error {
	if delaySeconds <= 0 {
		delaySeconds = 3
	}
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(delaySeconds*10))

	respCode, _, err := c.sendCommand(CmdUnlock, buf, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot unlock door relay", respCode)
	}
	return nil
}

// GetLockState queries the current door sensor state (open/closed).
func (c *Client) GetLockState() (bool, error) {
	respCode, _, err := c.sendCommand(CmdDoorStateRrq, nil, 8)
	if err != nil {
		return false, err
	}
	return respCode == CmdAckOk, nil
}

// TestVoice plays a built-in voice prompt on the device speaker (0: Thank you, 1: Wrong password, etc.).
func (c *Client) TestVoice(index int) error {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(index))

	respCode, _, err := c.sendCommand(CmdTestVoice, buf, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot play voice prompt", respCode)
	}
	return nil
}

// WriteLCD displays custom text on the device LCD screen.
func (c *Client) WriteLCD(lineNumber int, text string) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, int16(lineNumber))
	buf.WriteByte(0)
	buf.WriteByte(' ')
	buf.WriteString(text)

	respCode, _, err := c.sendCommand(CmdWriteLcd, buf.Bytes(), 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot write to LCD", respCode)
	}
	return nil
}

// ClearLCD clears the LCD screen text.
func (c *Client) ClearLCD() error {
	respCode, _, err := c.sendCommand(CmdClearLcd, nil, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot clear LCD", respCode)
	}
	return nil
}

// RefreshData refreshes device internal memory state and flush pending changes.
func (c *Client) RefreshData() error {
	respCode, _, err := c.sendCommand(CmdRefreshData, nil, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot refresh internal data", respCode)
	}
	return nil
}

// FreeData frees machine internal transmission buffers.
func (c *Client) FreeData() error {
	respCode, _, err := c.sendCommand(CmdFreeData, nil, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot free data buffer", respCode)
	}
	return nil
}

// GetDeviceInfo retrieves complete hardware, firmware, network and capacity details.
func (c *Client) GetDeviceInfo() (*DeviceInfo, error) {
	fw, _ := c.GetFirmwareVersion()
	sn, _ := c.GetSerialNumber()
	pf, _ := c.GetPlatform()
	mac, _ := c.GetMAC()
	name, _ := c.GetDeviceName()
	faceVer, _ := c.GetFaceVersion()
	fpVer, _ := c.GetFPVersion()
	extFmt, _ := c.GetExtendFmt()
	userExtFmt, _ := c.GetUserExtendFmt()
	faceFun, _ := c.GetFaceFunOn()
	oldFw, _ := c.GetCompatOldFirmware()
	pinW, _ := c.GetPinWidth()
	netParams, _ := c.GetNetworkParams()
	sizes, _ := c.ReadSizes()

	info := &DeviceInfo{
		FirmwareVersion:   fw,
		SerialNumber:      sn,
		Platform:          pf,
		MAC:               mac,
		DeviceName:        name,
		FaceVersion:       faceVer,
		FPVersion:         fpVer,
		ExtendFmt:         extFmt,
		UserExtendFmt:     userExtFmt,
		FaceFunOn:         faceFun,
		CompatOldFirmware: oldFw,
		PinWidth:          pinW,
	}
	if netParams != nil {
		info.Network = *netParams
	}
	if sizes != nil {
		info.Sizes = *sizes
	}
	return info, nil
}
