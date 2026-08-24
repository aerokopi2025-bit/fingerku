package zk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
)

// GetUsers retrieves all users enrolled in the machine.
func (c *Client) GetUsers() ([]User, error) {
	sizes, err := c.ReadSizes()
	if err != nil {
		return nil, err
	}

	if sizes.Users == 0 {
		c.mu.Lock()
		c.nextUID = 1
		c.nextUserID = "1"
		c.mu.Unlock()
		return []User{}, nil
	}

	userData, err := c.ReadWithBuffer(CmdUserTempRrq, FctUser, 0)
	if err != nil {
		return nil, err
	}

	if len(userData) <= 4 {
		return []User{}, nil
	}

	totalSize := int(binary.LittleEndian.Uint32(userData[:4]))
	packetSize := totalSize / sizes.Users
	if packetSize != 28 && packetSize != 72 {
		packetSize = 72 // default fallback to ZK8
	}

	c.mu.Lock()
	c.userPacketSize = packetSize
	c.mu.Unlock()

	userData = userData[4:]
	var users []User
	var maxUID uint16 = 0

	if packetSize == 28 {
		for len(userData) >= 28 {
			chunk := userData[:28]
			userData = userData[28:]

			uid := binary.LittleEndian.Uint16(chunk[0:2])
			privilege := int(chunk[2])
			pwd := CleanCString(chunk[3:8])
			name := CleanCString(chunk[8:16])
			card := binary.LittleEndian.Uint32(chunk[16:20])
			gid := int(chunk[21])
			userIDNum := binary.LittleEndian.Uint32(chunk[24:28])
			userID := fmt.Sprintf("%d", userIDNum)

			if uid > maxUID {
				maxUID = uid
			}
			if name == "" {
				name = fmt.Sprintf("NN-%s", userID)
			}

			users = append(users, User{
				UID:       uid,
				Name:      name,
				Privilege: privilege,
				Password:  pwd,
				GroupID:   strconv.Itoa(gid),
				UserID:    userID,
				Card:      card,
			})
		}
	} else {
		for len(userData) >= 72 {
			chunk := userData[:72]
			userData = userData[72:]

			uid := binary.LittleEndian.Uint16(chunk[0:2])
			privilege := int(chunk[2])
			pwd := CleanCString(chunk[3:11])
			name := CleanCString(chunk[11:35])
			card := binary.LittleEndian.Uint32(chunk[35:39])
			gid := CleanCString(chunk[40:47])
			userID := CleanCString(chunk[48:72])

			if uid > maxUID {
				maxUID = uid
			}
			if name == "" {
				name = fmt.Sprintf("NN-%s", userID)
			}

			users = append(users, User{
				UID:       uid,
				Name:      name,
				Privilege: privilege,
				Password:  pwd,
				GroupID:   gid,
				UserID:    userID,
				Card:      card,
			})
		}
	}

	maxUID++
	c.mu.Lock()
	c.nextUID = maxUID
	c.nextUserID = strconv.Itoa(int(maxUID))
	for {
		found := false
		for _, u := range users {
			if u.UserID == c.nextUserID {
				found = true
				maxUID++
				c.nextUserID = strconv.Itoa(int(maxUID))
				break
			}
		}
		if !found {
			break
		}
	}
	c.mu.Unlock()

	return users, nil
}

// SetUser enrolls a new user or updates an existing user profile on the machine.
func (c *Client) SetUser(u User) error {
	c.mu.Lock()
	if u.UID == 0 {
		u.UID = c.nextUID
	}
	if u.UserID == "" {
		u.UserID = fmt.Sprintf("%d", u.UID)
	}
	pktSize := c.userPacketSize
	c.mu.Unlock()

	var cmdBytes []byte

	if pktSize == 28 {
		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.LittleEndian, u.UID)
		buf.WriteByte(byte(u.Privilege))

		pwd := make([]byte, 5)
		copy(pwd, u.Password)
		buf.Write(pwd)

		name := make([]byte, 8)
		copy(name, u.Name)
		buf.Write(name)

		_ = binary.Write(buf, binary.LittleEndian, u.Card)

		gid, _ := strconv.Atoi(u.GroupID)
		buf.WriteByte(byte(gid))
		buf.WriteByte(0) // pad

		_ = binary.Write(buf, binary.LittleEndian, uint16(0)) // timezone

		uidNum, _ := strconv.Atoi(u.UserID)
		_ = binary.Write(buf, binary.LittleEndian, uint32(uidNum))

		cmdBytes = buf.Bytes()
	} else {
		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.LittleEndian, u.UID)
		buf.WriteByte(byte(u.Privilege))

		pwd := make([]byte, 8)
		copy(pwd, u.Password)
		buf.Write(pwd)

		name := make([]byte, 24)
		copy(name, u.Name)
		buf.Write(name)

		_ = binary.Write(buf, binary.LittleEndian, u.Card)
		buf.WriteByte(0) // pad

		gid := make([]byte, 7)
		copy(gid, u.GroupID)
		buf.Write(gid)
		buf.WriteByte(0) // pad

		userId := make([]byte, 24)
		copy(userId, u.UserID)
		buf.Write(userId)

		cmdBytes = buf.Bytes()
	}

	respCode, _, err := c.sendCommand(CmdUserWrq, cmdBytes, 1024)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError(fmt.Sprintf("cannot set user UID=%d, UserID=%s", u.UID, u.UserID), respCode)
	}

	_ = c.RefreshData()

	c.mu.Lock()
	if c.nextUID == u.UID {
		c.nextUID++
	}
	if c.nextUserID == u.UserID {
		c.nextUserID = strconv.Itoa(int(c.nextUID))
	}
	c.mu.Unlock()

	return nil
}

// DeleteUser removes a user record from the device by UID or UserID.
func (c *Client) DeleteUser(uid uint16, userID string) error {
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
			return fmt.Errorf("zk: user with user_id '%s' not found", userID)
		}
	}

	buf := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf, uid)

	respCode, _, err := c.sendCommand(CmdDeleteUser, buf, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError(fmt.Sprintf("cannot delete user UID=%d", uid), respCode)
	}

	_ = c.RefreshData()

	c.mu.Lock()
	if uid == c.nextUID-1 {
		c.nextUID = uid
	}
	c.mu.Unlock()

	return nil
}

// ClearAdmin resets all administrator privileges to regular user on the machine.
func (c *Client) ClearAdmin() error {
	respCode, _, err := c.sendCommand(CmdClearAdmin, nil, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("cannot clear admin privileges", respCode)
	}
	return c.RefreshData()
}
