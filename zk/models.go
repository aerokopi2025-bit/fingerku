package zk

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// User represents a user stored in the ZKTeco biometric machine.
type User struct {
	UID       uint16 `json:"uid"`
	Name      string `json:"name"`
	Privilege int    `json:"privilege"`
	Password  string `json:"password"`
	GroupID   string `json:"group_id"`
	UserID    string `json:"user_id"`
	Card      uint32 `json:"card"`
}

// IsDisabled checks if the user account is disabled.
func (u *User) IsDisabled() bool {
	return (u.Privilege & 1) != 0
}

// IsEnabled checks if the user account is active/enabled.
func (u *User) IsEnabled() bool {
	return !u.IsDisabled()
}

// UserType returns the raw user role bitmask.
func (u *User) UserType() int {
	return u.Privilege & 0x0E
}

// PrivilegeName returns a human-readable role name for the user.
func (u *User) PrivilegeName() string {
	role := "User"
	switch u.UserType() {
	case UserAdmin:
		role = "Admin"
	case UserManager:
		role = "Manager"
	case UserEnroller:
		role = "Enroller"
	}
	if u.IsDisabled() {
		role += " (DISABLED)"
	}
	return role
}

// String provides a formatted string representation of User.
func (u *User) String() string {
	return fmt.Sprintf("<User: UID=%d, Name='%s', UserID='%s', Role=%s, Card=%d>",
		u.UID, u.Name, u.UserID, u.PrivilegeName(), u.Card)
}

// Repack29 serializes user data for ZK6 high-rate buffer (with 0x02 prefix, 29 bytes).
func (u *User) Repack29() []byte {
	buf := new(bytes.Buffer)
	buf.WriteByte(2) // Prefix 0x02
	_ = binary.Write(buf, binary.LittleEndian, u.UID)
	buf.WriteByte(byte(u.Privilege))

	// Password (5 bytes)
	pwd := make([]byte, 5)
	copy(pwd, u.Password)
	buf.Write(pwd)

	// Name (8 bytes)
	name := make([]byte, 8)
	copy(name, u.Name)
	buf.Write(name)

	// Card (4 bytes uint32)
	_ = binary.Write(buf, binary.LittleEndian, u.Card)

	// Padding & Group ID
	buf.WriteByte(0) // pad 'x'
	gid, _ := strconv.Atoi(u.GroupID)
	buf.WriteByte(byte(gid))

	// Timezone (int16 = 0)
	_ = binary.Write(buf, binary.LittleEndian, int16(0))

	// User ID (uint32)
	uidNum, _ := strconv.Atoi(u.UserID)
	_ = binary.Write(buf, binary.LittleEndian, uint32(uidNum))

	return buf.Bytes()
}

// Repack73 serializes user data for ZK8 high-rate buffer (with 0x02 prefix, 73 bytes).
func (u *User) Repack73() []byte {
	buf := new(bytes.Buffer)
	buf.WriteByte(2) // Prefix 0x02
	_ = binary.Write(buf, binary.LittleEndian, u.UID)
	buf.WriteByte(byte(u.Privilege))

	// Password (8 bytes)
	pwd := make([]byte, 8)
	copy(pwd, u.Password)
	buf.Write(pwd)

	// Name (24 bytes)
	name := make([]byte, 24)
	copy(name, u.Name)
	buf.Write(name)

	// Card (4 bytes uint32)
	_ = binary.Write(buf, binary.LittleEndian, u.Card)

	// Flag / 1 byte (1)
	buf.WriteByte(1)

	// Group ID (7 bytes + 1 byte pad)
	gid := make([]byte, 7)
	copy(gid, u.GroupID)
	buf.Write(gid)
	buf.WriteByte(0) // pad 'x'

	// User ID (24 bytes)
	userId := make([]byte, 24)
	copy(userId, u.UserID)
	buf.Write(userId)

	return buf.Bytes()
}

// Attendance represents a single attendance event / punch log record.
type Attendance struct {
	UID       int       `json:"uid"`
	UserID    string    `json:"user_id"`
	Timestamp time.Time `json:"timestamp"`
	Status    int       `json:"status"`
	Punch     int       `json:"punch"`
}

// StatusName returns a human-readable punch state description.
func (a *Attendance) StatusName() string {
	switch a.Status {
	case 0:
		return "Check-In"
	case 1:
		return "Check-Out"
	case 2:
		return "Break-Out"
	case 3:
		return "Break-In"
	case 4:
		return "OT-In"
	case 5:
		return "OT-Out"
	default:
		return fmt.Sprintf("Status(%d)", a.Status)
	}
}

// String provides a formatted string representation of Attendance.
func (a *Attendance) String() string {
	return fmt.Sprintf("<Attendance: UserID=%s, Time=%s, Status=%s, Punch=%d, UID=%d>",
		a.UserID, a.Timestamp.Format("2006-01-02 15:04:05"), a.StatusName(), a.Punch, a.UID)
}

// Finger represents biometric fingerprint template data.
type Finger struct {
	UID      int    `json:"uid"`
	FID      int    `json:"fid"` // Finger ID (0-9)
	Valid    int    `json:"valid"`
	Template []byte `json:"-"`
	Size     int    `json:"size"`
}

// HexTemplate returns the fingerprint template as a hex string.
func (f *Finger) HexTemplate() string {
	return hex.EncodeToString(f.Template)
}

// Mark returns a short truncated hex marker for display.
func (f *Finger) Mark() string {
	if len(f.Template) < 16 {
		return hex.EncodeToString(f.Template)
	}
	head := hex.EncodeToString(f.Template[:8])
	tail := hex.EncodeToString(f.Template[len(f.Template)-8:])
	return head + "..." + tail
}

// Repack packs the complete template struct with header (<HHbb + template).
func (f *Finger) Repack() []byte {
	size := len(f.Template)
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, uint16(size+6))
	_ = binary.Write(buf, binary.LittleEndian, uint16(f.UID))
	buf.WriteByte(byte(f.FID))
	buf.WriteByte(byte(f.Valid))
	buf.Write(f.Template)
	return buf.Bytes()
}

// RepackOnly packs only the size header (<H) and template bytes.
func (f *Finger) RepackOnly() []byte {
	size := len(f.Template)
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, uint16(size))
	buf.Write(f.Template)
	return buf.Bytes()
}

// String provides a formatted string representation of Finger.
func (f *Finger) String() string {
	return fmt.Sprintf("<Finger: UID=%d, FID=%d, Size=%d, Valid=%d, Mark=%s>",
		f.UID, f.FID, f.Size, f.Valid, f.Mark())
}

// Sizes contains memory usage, capacity, and current counts from the machine.
type Sizes struct {
	Users            int `json:"users"`
	Fingers          int `json:"fingers"`
	Records          int `json:"records"`
	Cards            int `json:"cards"`
	FingersCap       int `json:"fingers_capacity"`
	UsersCap         int `json:"users_capacity"`
	RecordsCap       int `json:"records_capacity"`
	FingersAvailable int `json:"fingers_available"`
	UsersAvailable   int `json:"users_available"`
	RecordsAvailable int `json:"records_available"`
	Faces            int `json:"faces"`
	FacesCap         int `json:"faces_capacity"`
}

func (s *Sizes) String() string {
	return fmt.Sprintf("Users: %d/%d, Fingers: %d/%d, Records: %d/%d, Faces: %d/%d, Cards: %d",
		s.Users, s.UsersCap, s.Fingers, s.FingersCap, s.Records, s.RecordsCap, s.Faces, s.FacesCap, s.Cards)
}

// NetworkParams contains the device IP, Netmask, and Gateway settings.
type NetworkParams struct {
	IP      string `json:"ip"`
	Mask    string `json:"mask"`
	Gateway string `json:"gateway"`
}

// DeviceInfo aggregates general information and capabilities of the machine.
type DeviceInfo struct {
	FirmwareVersion   string        `json:"firmware_version"`
	SerialNumber      string        `json:"serial_number"`
	Platform          string        `json:"platform"`
	MAC               string        `json:"mac"`
	DeviceName        string        `json:"device_name"`
	FaceVersion       int           `json:"face_version"`
	FPVersion         int           `json:"fp_version"`
	ExtendFmt         int           `json:"extend_fmt"`
	UserExtendFmt     int           `json:"user_extend_fmt"`
	FaceFunOn         int           `json:"face_fun_on"`
	CompatOldFirmware int           `json:"compat_old_firmware"`
	PinWidth          int           `json:"pin_width"`
	Network           NetworkParams `json:"network"`
	Sizes             Sizes         `json:"sizes"`
}

// CleanCString removes trailing null bytes and whitespace.
func CleanCString(b []byte) string {
	if idx := bytes.IndexByte(b, 0); idx != -1 {
		b = b[:idx]
	}
	return strings.TrimSpace(string(b))
}
