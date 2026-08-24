package zk

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// UserTemplatesPair binds a User with their registered biometric Finger templates.
type UserTemplatesPair struct {
	User    User
	Fingers []Finger
}

// GetTemplates downloads all enrolled fingerprint templates from the device database.
func (c *Client) GetTemplates() ([]Finger, error) {
	sizes, err := c.ReadSizes()
	if err != nil {
		return nil, err
	}

	if sizes.Fingers == 0 {
		return []Finger{}, nil
	}

	templateData, err := c.ReadWithBuffer(CmdDbRrq, FctFingerTmp, 0)
	if err != nil {
		return nil, err
	}

	if len(templateData) < 4 {
		return []Finger{}, nil
	}

	totalSize := int(binary.LittleEndian.Uint32(templateData[:4]))
	templateData = templateData[4:]

	var templates []Finger

	for totalSize > 0 && len(templateData) >= 6 {
		size := int(binary.LittleEndian.Uint16(templateData[0:2]))
		uid := binary.LittleEndian.Uint16(templateData[2:4])
		fid := int(templateData[4])
		valid := int(templateData[5])

		if size < 6 || len(templateData) < size {
			break
		}

		templateBytes := make([]byte, size-6)
		copy(templateBytes, templateData[6:size])

		templates = append(templates, Finger{
			UID:      int(uid),
			FID:      fid,
			Valid:    valid,
			Template: templateBytes,
			Size:     len(templateBytes),
		})

		templateData = templateData[size:]
		totalSize -= size
	}

	return templates, nil
}

// GetUserTemplate retrieves a specific finger template for a user by UID/UserID and finger index (0-9).
func (c *Client) GetUserTemplate(uid uint16, fid int, userID string) (*Finger, error) {
	if uid == 0 && userID != "" {
		users, err := c.GetUsers()
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if u.UserID == userID {
				uid = u.UID
				break
			}
		}
		if uid == 0 {
			return nil, fmt.Errorf("zk: user '%s' not found", userID)
		}
	}

	cmdBuf := new(bytes.Buffer)
	_ = binary.Write(cmdBuf, binary.LittleEndian, int16(uid))
	cmdBuf.WriteByte(byte(fid))

	respCode, data, err := c.sendCommand(CmdGetUserTemp, cmdBuf.Bytes(), 1024+8)
	if err != nil {
		return nil, err
	}
	if respCode != CmdAckOk && respCode != CmdData {
		return nil, NewResponseError(fmt.Sprintf("cannot get template for UID=%d, FID=%d", uid, fid), respCode)
	}

	// Clean trailing null padding if present
	if len(data) > 6 && bytes.HasSuffix(data, []byte{0, 0, 0, 0, 0, 0}) {
		data = data[:len(data)-6]
	}

	return &Finger{
		UID:      int(uid),
		FID:      fid,
		Valid:    1,
		Template: data,
		Size:     len(data),
	}, nil
}

// SaveUserTemplate writes a user and their fingerprint templates to the machine.
func (c *Client) SaveUserTemplate(user User, fingers []Finger) error {
	return c.SaveUserTemplatesHighRate([]UserTemplatesPair{
		{
			User:    user,
			Fingers: fingers,
		},
	})
}

// SaveUserTemplatesHighRate writes batch users and biometric templates in high-rate mode.
func (c *Client) SaveUserTemplatesHighRate(items []UserTemplatesPair) error {
	c.mu.Lock()
	pktSize := c.userPacketSize
	c.mu.Unlock()

	var upack []byte
	var fpack []byte
	var table []byte

	const fnum byte = 0x10
	tStart := uint32(0)

	for _, item := range items {
		if pktSize == 28 {
			upack = append(upack, item.User.Repack29()...)
		} else {
			upack = append(upack, item.User.Repack73()...)
		}

		for _, finger := range item.Fingers {
			tfp := finger.RepackOnly()
			tblBuf := new(bytes.Buffer)
			tblBuf.WriteByte(2) // flag
			_ = binary.Write(tblBuf, binary.LittleEndian, item.User.UID)
			tblBuf.WriteByte(fnum + byte(finger.FID))
			_ = binary.Write(tblBuf, binary.LittleEndian, tStart)

			table = append(table, tblBuf.Bytes()...)
			tStart += uint32(len(tfp))
			fpack = append(fpack, tfp...)
		}
	}

	headBuf := new(bytes.Buffer)
	_ = binary.Write(headBuf, binary.LittleEndian, uint32(len(upack)))
	_ = binary.Write(headBuf, binary.LittleEndian, uint32(len(table)))
	_ = binary.Write(headBuf, binary.LittleEndian, uint32(len(fpack)))

	fullPacket := append(headBuf.Bytes(), upack...)
	fullPacket = append(fullPacket, table...)
	fullPacket = append(fullPacket, fpack...)

	if err := c.SendWithBuffer(fullPacket); err != nil {
		return err
	}

	cmdStr := new(bytes.Buffer)
	_ = binary.Write(cmdStr, binary.LittleEndian, uint32(12))
	_ = binary.Write(cmdStr, binary.LittleEndian, uint16(0))
	_ = binary.Write(cmdStr, binary.LittleEndian, uint16(8))

	respCode, _, err := c.sendCommand(CmdSaveUserTemps, cmdStr.Bytes(), 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot save user templates high-rate", respCode)
	}

	return c.RefreshData()
}

// DeleteUserTemplate deletes a specific finger template from the machine.
func (c *Client) DeleteUserTemplate(uid uint16, fid int, userID string) error {
	c.mu.Lock()
	isTCP := c.tcp
	c.mu.Unlock()

	if isTCP && userID != "" {
		buf := new(bytes.Buffer)
		userIdBytes := make([]byte, 24)
		copy(userIdBytes, userID)
		buf.Write(userIdBytes)
		buf.WriteByte(byte(fid))

		respCode, _, err := c.sendCommand(CmdDelUserTemp, buf.Bytes(), 8)
		if err != nil {
			return err
		}
		if respCode != CmdAckOk {
			return NewResponseError(fmt.Sprintf("cannot delete template UserID=%s, FID=%d", userID, fid), respCode)
		}
		return c.RefreshData()
	}

	if uid == 0 && userID != "" {
		users, err := c.GetUsers()
		if err != nil {
			return err
		}
		for _, u := range users {
			if u.UserID == userID {
				uid = u.UID
				break
			}
		}
		if uid == 0 {
			return fmt.Errorf("zk: user '%s' not found", userID)
		}
	}

	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, int16(uid))
	buf.WriteByte(byte(fid))

	respCode, _, err := c.sendCommand(CmdDeleteUserTemp, buf.Bytes(), 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError(fmt.Sprintf("cannot delete template UID=%d, FID=%d", uid, fid), respCode)
	}

	return c.RefreshData()
}
