package zk

// Max unsigned 16-bit integer
const USHRT_MAX uint16 = 65535

// Command opcodes for ZKTeco protocol
const (
	CmdDbRrq          uint16 = 7   // Read data from machine
	CmdUserWrq        uint16 = 8   // Upload user information
	CmdUserTempRrq    uint16 = 9   // Read fingerprint template / user data
	CmdUserTempWrq    uint16 = 10  // Upload fingerprint template
	CmdOptionsRrq     uint16 = 11  // Read configuration parameter
	CmdOptionsWrq     uint16 = 12  // Set configuration parameter
	CmdAttLogRrq      uint16 = 13  // Read attendance records
	CmdClearData      uint16 = 14  // Clear all data
	CmdClearAttLog    uint16 = 15  // Clear attendance records
	CmdDeleteUser     uint16 = 18  // Delete user
	CmdDeleteUserTemp uint16 = 19  // Delete fingerprint template
	CmdClearAdmin     uint16 = 20  // Clear admin privileges
	CmdUserGrpRrq     uint16 = 21  // Read user grouping
	CmdUserGrpWrq     uint16 = 22  // Set user grouping
	CmdUserTzRrq      uint16 = 23  // Read user timezone
	CmdUserTzWrq      uint16 = 24  // Write user timezone
	CmdGrpTzRrq       uint16 = 25  // Read group timezone
	CmdGrpTzWrq       uint16 = 26  // Write group timezone
	CmdTzRrq          uint16 = 27  // Read timezone
	CmdTzWrq          uint16 = 28  // Write timezone
	CmdUlgRrq         uint16 = 29  // Read unlock combination
	CmdUlgWrq         uint16 = 30  // Write unlock combination
	CmdUnlock         uint16 = 31  // Unlock door
	CmdClearAcc       uint16 = 32  // Restore access control to default
	CmdClearOpLog     uint16 = 33  // Delete operation records
	CmdOpLogRrq       uint16 = 34  // Read operation records
	CmdGetFreeSizes   uint16 = 50  // Obtain machine memory/capacity info
	CmdEnableClock    uint16 = 57  // Ensure machine normal work state
	CmdStartVerify    uint16 = 60  // Start verification condition
	CmdStartEnroll    uint16 = 61  // Start user enrollment
	CmdCancelCapture  uint16 = 62  // Cancel capture / return to waiting
	CmdStateRrq       uint16 = 64  // Get machine condition
	CmdWriteLcd       uint16 = 66  // Write text to LCD
	CmdClearLcd       uint16 = 67  // Clear LCD screen
	CmdGetPinWidth    uint16 = 69  // Obtain user ID pin width
	CmdSmsWrq         uint16 = 70  // Upload short message
	CmdSmsRrq         uint16 = 71  // Download short message
	CmdDeleteSms      uint16 = 72  // Delete short message
	CmdUdataWrq       uint16 = 73  // Set user short message
	CmdDeleteUdata    uint16 = 74  // Delete user short message
	CmdDoorStateRrq   uint16 = 75  // Obtain door condition
	CmdWriteMifare    uint16 = 76  // Write Mifare card
	CmdEmptyMifare    uint16 = 78  // Clear Mifare card
	CmdGetUserTemp    uint16 = 88  // Get specific user template (uid, fid)
	CmdSaveUserTemps  uint16 = 110 // Save user and multiple templates
	CmdDelUserTemp    uint16 = 134 // Delete specific user template (uid, fid)

	CmdGetTime  uint16 = 201 // Obtain machine time
	CmdSetTime  uint16 = 202 // Set machine time
	CmdRegEvent uint16 = 500 // Register real-time event

	CmdConnect       uint16 = 1000 // Connection request
	CmdExit          uint16 = 1001 // Disconnection request
	CmdEnableDevice  uint16 = 1002 // Enable device
	CmdDisableDevice uint16 = 1003 // Disable device (lock screen)
	CmdRestart       uint16 = 1004 // Restart device
	CmdPowerOff      uint16 = 1005 // Power off device
	CmdSleep         uint16 = 1006 // Sleep mode
	CmdResume        uint16 = 1007 // Wake up from sleep
	CmdCaptureFinger uint16 = 1009 // Capture fingerprint image
	CmdTestTemp      uint16 = 1011 // Test if fingerprint exists
	CmdCaptureImage  uint16 = 1012 // Capture entire image
	CmdRefreshData   uint16 = 1013 // Refresh machine internal data
	CmdRefreshOption uint16 = 1014 // Refresh configuration parameter
	CmdTestVoice     uint16 = 1017 // Play voice prompt
	CmdGetVersion    uint16 = 1100 // Obtain firmware version
	CmdChangeSpeed   uint16 = 1101 // Change transmission speed
	CmdAuth          uint16 = 1102 // Connection authorization
	CmdPrepareData   uint16 = 1500 // Prepare to transmit data
	CmdData          uint16 = 1501 // Transmit data packet
	CmdFreeData      uint16 = 1502 // Clear machine buffer
	CmdPrepareBuffer uint16 = 1503 // Initialize buffer for partial reads
	CmdReadBuffer    uint16 = 1504 // Read a partial chunk of data from buffer
)

// Response return codes
const (
	CmdAckOk        uint16 = 2000   // Order performed successfully
	CmdAckError     uint16 = 2001   // Order failed
	CmdAckData      uint16 = 2002   // Return data
	CmdAckRetry     uint16 = 2003   // Registered event occurred
	CmdAckRepeat    uint16 = 2004   // Not available
	CmdAckUnauth    uint16 = 2005   // Connection unauthorized
	CmdAckUnknown   uint16 = 0xffff // Unknown order
	CmdAckErrorCmd  uint16 = 0xfffd // Order false
	CmdAckErrorInit uint16 = 0xfffc // Not initialized
	CmdAckErrorData uint16 = 0xfffb // Data error
)

// Event flags for CMD_REG_EVENT
const (
	EfAttLog       uint32 = 1        // Real-time verification (attendance log)
	EfFinger       uint32 = (1 << 1) // Real-time finger press
	EfEnrollUser   uint32 = (1 << 2) // Real-time user enrollment
	EfEnrollFinger uint32 = (1 << 3) // Real-time fingerprint enrollment
	EfButton       uint32 = (1 << 4) // Real-time button press
	EfUnlock       uint32 = (1 << 5) // Real-time unlock
	EfVerify       uint32 = (1 << 7) // Real-time verify fingerprint
	EfFpftr        uint32 = (1 << 8) // Real-time fingerprint minutiae capture
	EfAlarm        uint32 = (1 << 9) // Alarm signal
)

// User privilege levels
const (
	UserDefault  int = 0
	UserEnroller int = 2
	UserManager  int = 6
	UserAdmin    int = 14
)

// Data function codes
const (
	FctAttLog    uint16 = 1
	FctFingerTmp uint16 = 2
	FctOpLog     uint16 = 4
	FctUser      uint16 = 5
	FctSms       uint16 = 6
	FctUdata     uint16 = 7
	FctWorkCode  uint16 = 8
)

// TCP packet magic headers
const (
	MachinePrepareData1 uint16 = 20560 // 0x5050
	MachinePrepareData2 uint16 = 32130 // 0x7D82
)
