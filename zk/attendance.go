package zk

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// GetAttendance retrieves all attendance logs currently stored in the machine.
func (c *Client) GetAttendance() ([]Attendance, error) {
	sizes, err := c.ReadSizes()
	if err != nil {
		sizes = &Sizes{}
	}

	// Fetch users map for UID/UserID cross-referencing
	users, _ := c.GetUsers()
	uidMap := make(map[uint16]string)
	userMap := make(map[string]uint16)
	for _, u := range users {
		uidMap[u.UID] = u.UserID
		userMap[u.UserID] = u.UID
	}

	attData, err := c.ReadWithBuffer(CmdAttLogRrq, 0, 0)
	if err != nil {
		if sizes.Records == 0 {
			return []Attendance{}, nil
		}
		return nil, err
	}

	if len(attData) < 4 {
		return []Attendance{}, nil
	}

	totalSize := int(binary.LittleEndian.Uint32(attData[:4]))
	if totalSize == 0 {
		return []Attendance{}, nil
	}

	attData = attData[4:]
	var recordSize int
	if sizes.Records > 0 && totalSize%sizes.Records == 0 {
		recordSize = totalSize / sizes.Records
	}
	if recordSize != 8 && recordSize != 16 && recordSize != 40 {
		if len(attData)%40 == 0 {
			recordSize = 40
		} else if len(attData)%8 == 0 {
			recordSize = 8
		} else if len(attData)%16 == 0 {
			recordSize = 16
		} else {
			recordSize = 40 // default fallback
		}
	}

	var records []Attendance

	switch {
	case recordSize == 8:
		for len(attData) >= 8 {
			chunk := attData[:8]
			attData = attData[8:]

			uid := binary.LittleEndian.Uint16(chunk[0:2])
			status := int(chunk[2])
			timestamp := DecodeTimeBytes(chunk[3:7])
			punch := int(chunk[7])

			userID := uidMap[uid]
			if userID == "" {
				userID = fmt.Sprintf("%d", uid)
			}

			records = append(records, Attendance{
				UID:       int(uid),
				UserID:    userID,
				Timestamp: timestamp,
				Status:    status,
				Punch:     punch,
			})
		}

	case recordSize == 16:
		for len(attData) >= 16 {
			chunk := attData[:16]
			attData = attData[16:]

			userIDNum := binary.LittleEndian.Uint32(chunk[0:4])
			timestamp := DecodeTimeBytes(chunk[4:8])
			status := int(chunk[8])
			punch := int(chunk[9])

			userID := fmt.Sprintf("%d", userIDNum)
			uid := int(userMap[userID])
			if uid == 0 {
				uid = int(userIDNum)
			}

			records = append(records, Attendance{
				UID:       uid,
				UserID:    userID,
				Timestamp: timestamp,
				Status:    status,
				Punch:     punch,
			})
		}

	default: // 40 bytes or others (modern TFT)
		codeInit := []byte("\xff255\x00\x00\x00\x00\x00")
		if bytes.HasPrefix(attData, codeInit) {
			attData = attData[len(codeInit):]
		}

		stride := recordSize
		if stride < 40 {
			stride = 40
		}

		for len(attData) >= 40 {
			chunk := attData[:40]
			if len(attData) >= stride {
				attData = attData[stride:]
			} else {
				attData = attData[40:]
			}

			uid := binary.LittleEndian.Uint16(chunk[0:2])
			userID := CleanCString(chunk[2:26])
			status := int(chunk[26])
			timestamp := DecodeTimeBytes(chunk[27:31])
			punch := int(chunk[31])

			records = append(records, Attendance{
				UID:       int(uid),
				UserID:    userID,
				Timestamp: timestamp,
				Status:    status,
				Punch:     punch,
			})
		}
	}

	return records, nil
}
