package zk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"time"
)

// ReadWithBuffer performs chunked reading of large data from the device buffer.
func (c *Client) ReadWithBuffer(command uint16, fct uint16, ext uint32) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var maxChunk int
	if c.tcp {
		maxChunk = 0xFFC0 // 65472 bytes
	} else {
		maxChunk = 16 * 1024 // 16384 bytes
	}

	// Prepare buffer command string: <bhii (1 byte flag=1, 2 bytes command, 4 bytes fct, 4 bytes ext)
	cmdBuf := new(bytes.Buffer)
	cmdBuf.WriteByte(1)
	_ = binary.Write(cmdBuf, binary.LittleEndian, command)
	_ = binary.Write(cmdBuf, binary.LittleEndian, int32(fct))
	_ = binary.Write(cmdBuf, binary.LittleEndian, int32(ext))

	respCode, data, err := c.rawSendCommand(CmdPrepareBuffer, cmdBuf.Bytes(), 1024)
	if err != nil {
		return nil, err
	}

	if respCode == CmdData {
		// Entire dataset returned in single response
		return data, nil
	}

	if respCode != CmdAckOk && respCode != CmdPrepareData {
		return nil, NewResponseError("prepare buffer not supported or failed", respCode)
	}

	if len(data) < 5 {
		return nil, ErrBufferEmpty
	}

	totalSize := int(binary.LittleEndian.Uint32(data[1:5]))
	if totalSize == 0 {
		return nil, nil
	}

	var result []byte
	start := 0
	packets := totalSize / maxChunk
	remain := totalSize % maxChunk

	for i := 0; i < packets; i++ {
		chunk, err := c.rawReadChunk(start, maxChunk)
		if err != nil {
			return nil, err
		}
		result = append(result, chunk...)
		start += maxChunk
	}

	if remain > 0 {
		chunk, err := c.rawReadChunk(start, remain)
		if err != nil {
			return nil, err
		}
		result = append(result, chunk...)
		start += remain
	}

	// Free buffer on device
	_, _, _ = c.rawSendCommand(CmdFreeData, nil, 8)

	return result, nil
}

// rawReadChunk fetches a specific chunk slice from the device buffer.
func (c *Client) rawReadChunk(start int, size int) ([]byte, error) {
	cmdBuf := new(bytes.Buffer)
	_ = binary.Write(cmdBuf, binary.LittleEndian, int32(start))
	_ = binary.Write(cmdBuf, binary.LittleEndian, int32(size))

	var lastErr error
	for retries := 0; retries < 3; retries++ {
		respCode, data, err := c.rawSendCommand(CmdReadBuffer, cmdBuf.Bytes(), size+32)
		if err != nil {
			lastErr = err
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if respCode == CmdData {
			return data, nil
		}

		if respCode == CmdPrepareData {
			chunkData, err := c.rawReceivePrepareData(data, size)
			if err == nil && len(chunkData) > 0 {
				return chunkData, nil
			}
			lastErr = err
		}
	}

	return nil, fmt.Errorf("zk: failed to read chunk [%d:%d]: %w", start, size, lastErr)
}

// rawReceivePrepareData handles reading when CMD_PREPARE_DATA is returned.
func (c *Client) rawReceivePrepareData(initialData []byte, expectedSize int) ([]byte, error) {
	if c.tcp {
		// Read 8-byte TCP top header of CMD_DATA packet
		topHeader := make([]byte, 8)
		if _, err := io.ReadFull(c.conn, topHeader); err != nil {
			return nil, &NetworkError{Op: "read data tcp top", Err: err}
		}

		payloadLen, err := TestTCPTop(topHeader)
		if err != nil {
			return nil, err
		}

		// Read payloadLen bytes (8 bytes ZK header + actual data)
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(c.conn, payload); err != nil {
			return nil, &NetworkError{Op: "read data payload", Err: err}
		}

		if len(payload) < 8 {
			return nil, fmt.Errorf("%w: data payload shorter than 8 bytes", ErrInvalidTcpHeader)
		}

		respCode := binary.LittleEndian.Uint16(payload[0:2])
		if respCode != CmdData {
			return nil, fmt.Errorf("zk: expected CmdData (1501), got response code %d", respCode)
		}

		chunkData := payload[8:]

		// If data was split across TCP frames
		needed := expectedSize - len(chunkData)
		if needed > 0 {
			extra := make([]byte, needed)
			if _, err := io.ReadFull(c.conn, extra); err != nil {
				return nil, &NetworkError{Op: "read extra data", Err: err}
			}
			chunkData = append(chunkData, extra...)
		}

		// Read CMD_ACK_OK packet (8 bytes TCP top + 8 bytes ZK header = 16 bytes)
		ackBuf := make([]byte, 16)
		if _, err := io.ReadFull(c.conn, ackBuf); err != nil {
			if c.verbose {
				log.Printf("[zk] Notice: ACK reading: %v", err)
			}
		}

		return chunkData, nil
	}

	// UDP Transport
	var collected []byte
	remaining := expectedSize
	for remaining > 0 {
		buf := make([]byte, 1024+8)
		n, err := c.conn.Read(buf)
		if err != nil {
			return nil, err
		}
		if n < 8 {
			break
		}
		respCode := binary.LittleEndian.Uint16(buf[0:2])
		if respCode == CmdData {
			collected = append(collected, buf[8:n]...)
			remaining -= (n - 8)
		} else if respCode == CmdAckOk {
			break
		}
	}
	return collected, nil
}

// SendWithBuffer sends large payload data by breaking it into chunks.
func (c *Client) SendWithBuffer(buffer []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	const maxChunk = 1024
	size := len(buffer)

	// Free data on device first
	_, _, _ = c.rawSendCommand(CmdFreeData, nil, 8)

	// Prepare data command
	prepBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(prepBuf, uint32(size))
	respCode, _, err := c.rawSendCommand(CmdPrepareData, prepBuf, 8)
	if err != nil {
		return err
	}
	if respCode != CmdAckOk {
		return NewResponseError("can't prepare data buffer on device", respCode)
	}

	start := 0
	remain := size % maxChunk
	packets := (size - remain) / maxChunk

	for i := 0; i < packets; i++ {
		chunk := buffer[start : start+maxChunk]
		respCode, _, err := c.rawSendCommand(CmdData, chunk, 8)
		if err != nil {
			return err
		}
		if respCode != CmdAckOk {
			return NewResponseError("failed sending chunk", respCode)
		}
		start += maxChunk
	}

	if remain > 0 {
		chunk := buffer[start : start+remain]
		respCode, _, err := c.rawSendCommand(CmdData, chunk, 8)
		if err != nil {
			return err
		}
		if respCode != CmdAckOk {
			return NewResponseError("failed sending remaining chunk", respCode)
		}
	}

	return nil
}
