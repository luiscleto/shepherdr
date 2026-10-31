package herdr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sync"
	"time"
)

// Frame kinds of herdr-core's framed mode (schema/README.md, "Framed mode").
const (
	coreFrameData   = 0
	coreFrameCredit = 1
	coreFrameClose  = 2
	coreFrameResize = 3

	coreFrameHeaderBytes = 9
	coreFrameMaxPayload  = 16 << 20
	// Reader's older output is at most this many bytes of first paint.
	CoreMaxPaintBytes = 16 << 20
)

// In herdr-core 0.1.15 every first paint ends with the program's
// win32-input-mode, CSI ?9001h or CSI ?9001l, followed only by status
// reports. Core sends the paint as stream bytes in chunks of up to 16 KiB,
// with live output right after it in the same queue, so this marker is the
// only place the paint can be cut. Screen cells and titles can't carry an
// escape sequence, so the first marker on a fresh attach is the paint's own.
// This relies on core 0.1.15 behaviour and is validated per version.
var corePaintEndMarkers = [][]byte{[]byte("\x1b[?9001h"), []byte("\x1b[?9001l")}

// CorePaintEnd returns the length of the first paint at the start of a fresh
// attach stream, or -1 while the stream doesn't hold all of it yet.
func CorePaintEnd(stream []byte) int {
	end := -1
	for _, marker := range corePaintEndMarkers {
		if at := bytes.Index(stream, marker); at >= 0 && (end < 0 || at+len(marker) < end) {
			end = at + len(marker)
		}
	}
	return end
}

type coreFrame struct {
	channel uint32
	kind    byte
	payload []byte
}

func readCoreFrame(reader io.Reader) (coreFrame, error) {
	var header [coreFrameHeaderBytes]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return coreFrame{}, err
	}
	length := binary.LittleEndian.Uint32(header[5:9])
	if length > coreFrameMaxPayload {
		return coreFrame{}, fmt.Errorf("Herdr Core frame of %d bytes exceeds the protocol limit", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return coreFrame{}, err
	}
	return coreFrame{channel: binary.LittleEndian.Uint32(header[0:4]), kind: header[4], payload: payload}, nil
}

func encodeCoreFrame(channel uint32, kind byte, payload []byte) []byte {
	frame := make([]byte, coreFrameHeaderBytes+len(payload))
	binary.LittleEndian.PutUint32(frame[0:4], channel)
	frame[4] = kind
	binary.LittleEndian.PutUint32(frame[5:9], uint32(len(payload)))
	copy(frame[coreFrameHeaderBytes:], payload)
	return frame
}

// CoreAttachment is one terminal.attach stream on its own framed connection.
type CoreAttachment struct {
	Cols int
	Rows int

	conn    net.Conn
	reader  *bufio.Reader
	channel uint32
	events  uint32
	early   []coreFrame
	nextID  uint64

	writeMu sync.Mutex
	replyMu sync.Mutex
	replies map[uint64]chan coreResponse
	failed  error
}

// CoreStreamEvent is one thing that happened on an attach stream: output
// bytes, a new terminal size, or the end of the stream with core's reason.
type CoreStreamEvent struct {
	Data   []byte
	Cols   int
	Rows   int
	Closed bool
	Reason string
}

// Attach opens a framed connection and attaches to one terminal. An observer
// never types or resizes. With follow it also subscribes to the terminal's
// events so the stream reports size changes. Start the stream with Run.
func (c *CoreClient) Attach(ctx context.Context, address string, cols, rows int, observe, follow bool, history int) (*CoreAttachment, error) {
	dialer := net.Dialer{Timeout: requestTimeout}
	conn, err := dialer.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect to Herdr Core: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(requestTimeout))
	attachment := &CoreAttachment{conn: conn, reader: bufio.NewReader(conn), replies: make(map[uint64]chan coreResponse)}
	if err := attachment.open(c, address, cols, rows, observe, follow, history); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return attachment, nil
}

func (a *CoreAttachment) open(c *CoreClient, address string, cols, rows int, observe, follow bool, history int) error {
	request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 0, "method": "hello", "params": map[string]any{"generation": coreGeneration, "framed": true}})
	if _, err := a.conn.Write(append(request, '\n')); err != nil {
		return fmt.Errorf("write Herdr Core hello: %w", err)
	}
	line, err := a.reader.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("read Herdr Core hello: %w", err)
	}
	var response coreResponse
	var hello coreHello
	if err := json.Unmarshal(line, &response); err != nil {
		return fmt.Errorf("decode Herdr Core hello: %w", err)
	}
	if response.Error != nil {
		return &APIError{Code: response.Error.Data.Kind, Message: response.Error.Message}
	}
	if err := json.Unmarshal(response.Result, &hello); err != nil {
		return fmt.Errorf("decode Herdr Core hello: %w", err)
	}
	if err := c.checkHello(hello); err != nil {
		return err
	}
	if follow {
		var subscribed struct {
			Channel *uint32 `json:"channel"`
		}
		if err := a.setupCall("events.subscribe", map[string]any{"terminal": address}, &subscribed); err != nil {
			return err
		}
		if subscribed.Channel == nil {
			return errors.New("Herdr Core returned an unexpected subscription response")
		}
		a.events = *subscribed.Channel
	}
	var attached struct {
		Channel  *uint32 `json:"channel"`
		Terminal struct {
			Cols int `json:"cols"`
			Rows int `json:"rows"`
		} `json:"terminal"`
	}
	params := map[string]any{"terminal": address, "cols": cols, "rows": rows, "observe": observe, "history": history}
	if err := a.setupCall("terminal.attach", params, &attached); err != nil {
		return err
	}
	if attached.Channel == nil || *attached.Channel == 0 || *attached.Channel == a.events && follow {
		return errors.New("Herdr Core returned an unexpected attach response")
	}
	a.channel = *attached.Channel
	a.Cols, a.Rows = attached.Terminal.Cols, attached.Terminal.Rows
	return nil
}

// setupCall makes one call before Run starts reading. Stream frames that
// arrive first are kept for Run, in order.
func (a *CoreAttachment) setupCall(method string, params, result any) error {
	a.nextID++
	id := a.nextID
	if err := a.writeCall(id, method, params); err != nil {
		return err
	}
	for {
		frame, err := readCoreFrame(a.reader)
		if err != nil {
			return fmt.Errorf("read Herdr Core %s response: %w", method, err)
		}
		if frame.channel != 0 || frame.kind != coreFrameData {
			a.early = append(a.early, frame)
			continue
		}
		var response coreResponse
		if err := json.Unmarshal(frame.payload, &response); err != nil {
			return fmt.Errorf("decode Herdr Core %s response: %w", method, err)
		}
		if string(response.ID) != fmt.Sprint(id) {
			continue
		}
		if response.Error != nil {
			return &APIError{Code: response.Error.Data.Kind, Message: response.Error.Message}
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decode Herdr Core %s response: %w", method, err)
		}
		return nil
	}
}

func (a *CoreAttachment) writeCall(id uint64, method string, params any) error {
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	return a.writeFrame(0, coreFrameData, request)
}

func (a *CoreAttachment) writeFrame(channel uint32, kind byte, payload []byte) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	_ = a.conn.SetWriteDeadline(time.Now().Add(requestTimeout))
	_, err := a.conn.Write(encodeCoreFrame(channel, kind, payload))
	return err
}

// Run reads the stream until it ends, sending each event to handle in order.
// It returns when core closes the stream, the connection fails, or handle
// returns an error.
func (a *CoreAttachment) Run(handle func(CoreStreamEvent) error) error {
	err := a.run(handle)
	a.replyMu.Lock()
	a.failed = err
	if a.failed == nil {
		a.failed = errors.New("Herdr Core stream ended")
	}
	for id, reply := range a.replies {
		close(reply)
		delete(a.replies, id)
	}
	a.replyMu.Unlock()
	return err
}

func (a *CoreAttachment) run(handle func(CoreStreamEvent) error) error {
	var lines []byte
	for {
		var frame coreFrame
		if len(a.early) > 0 {
			frame, a.early = a.early[0], a.early[1:]
		} else {
			var err error
			if frame, err = readCoreFrame(a.reader); err != nil {
				return err
			}
		}
		switch {
		case frame.channel == 0 && frame.kind == coreFrameData:
			a.deliverReply(frame.payload)
		case frame.channel == a.channel && frame.kind == coreFrameData:
			if err := handle(CoreStreamEvent{Data: frame.payload}); err != nil {
				return err
			}
		case frame.channel == a.channel && frame.kind == coreFrameClose:
			var closed struct {
				Reason string `json:"reason"`
			}
			_ = json.Unmarshal(frame.payload, &closed)
			if closed.Reason == "" {
				closed.Reason = "closed"
			}
			return handle(CoreStreamEvent{Closed: true, Reason: closed.Reason})
		case a.events != 0 && frame.channel == a.events && frame.kind == coreFrameData:
			lines = append(lines, frame.payload...)
			if err := a.Credit(a.events, len(frame.payload)); err != nil {
				return err
			}
			for {
				end := bytes.IndexByte(lines, '\n')
				if end < 0 {
					break
				}
				line := lines[:end]
				lines = lines[end+1:]
				if cols, rows, ok := coreSizeEvent(line); ok {
					if err := handle(CoreStreamEvent{Cols: cols, Rows: rows}); err != nil {
						return err
					}
				}
			}
		case a.events != 0 && frame.channel == a.events && frame.kind == coreFrameClose:
			return errors.New("Herdr Core ended the terminal's event stream")
		}
	}
}

func coreSizeEvent(line []byte) (int, int, bool) {
	var notification struct {
		Method string `json:"method"`
		Params struct {
			Type string `json:"type"`
			Data struct {
				Cols int `json:"cols"`
				Rows int `json:"rows"`
			} `json:"data"`
		} `json:"params"`
	}
	if json.Unmarshal(line, &notification) != nil || notification.Method != "event" || notification.Params.Type != "size" {
		return 0, 0, false
	}
	return notification.Params.Data.Cols, notification.Params.Data.Rows, true
}

func (a *CoreAttachment) deliverReply(payload []byte) {
	var response coreResponse
	if json.Unmarshal(payload, &response) != nil {
		return
	}
	var id uint64
	if json.Unmarshal(response.ID, &id) != nil {
		return
	}
	a.replyMu.Lock()
	reply := a.replies[id]
	delete(a.replies, id)
	a.replyMu.Unlock()
	if reply != nil {
		reply <- response
	}
}

// Credit tells core the bytes on a channel were consumed.
func (a *CoreAttachment) Credit(channel uint32, bytes int) error {
	var payload [4]byte
	binary.LittleEndian.PutUint32(payload[:], uint32(bytes))
	return a.writeFrame(channel, coreFrameCredit, payload[:])
}

// CreditStream credits bytes consumed from the attach stream.
func (a *CoreAttachment) CreditStream(bytes int) error { return a.Credit(a.channel, bytes) }

// Write sends input bytes on the attach stream.
func (a *CoreAttachment) Write(data []byte) (int, error) {
	if err := a.writeFrame(a.channel, coreFrameData, data); err != nil {
		return 0, err
	}
	return len(data), nil
}

// Resize sets this viewer's size. Core ignores it for an observer.
func (a *CoreAttachment) Resize(cols, rows int) error {
	var payload [4]byte
	binary.LittleEndian.PutUint16(payload[0:2], uint16(cols))
	binary.LittleEndian.PutUint16(payload[2:4], uint16(rows))
	return a.writeFrame(a.channel, coreFrameResize, payload[:])
}

// ErrCoreKeysUnconfirmed means the request was written but no answer came.
var ErrCoreKeysUnconfirmed = errors.New("Herdr Core did not confirm the key")

// Keys asks core to press one named key in the terminal. It needs Run to be
// reading the connection. An APIError means core refused the key.
func (a *CoreAttachment) Keys(ctx context.Context, address, name string) error {
	reply := make(chan coreResponse, 1)
	a.replyMu.Lock()
	if a.failed != nil {
		a.replyMu.Unlock()
		return a.failed
	}
	a.nextID++
	id := a.nextID
	a.replies[id] = reply
	a.replyMu.Unlock()
	forget := func() {
		a.replyMu.Lock()
		delete(a.replies, id)
		a.replyMu.Unlock()
	}
	if err := a.writeCall(id, "terminal.keys", map[string]any{"terminal": address, "keys": []string{name}}); err != nil {
		forget()
		return fmt.Errorf("%w: %v", ErrCoreKeysUnconfirmed, err)
	}
	timer := time.NewTimer(requestTimeout)
	defer timer.Stop()
	select {
	case response, ok := <-reply:
		if !ok {
			return ErrCoreKeysUnconfirmed
		}
		if response.Error != nil {
			return &APIError{Code: response.Error.Data.Kind, Message: response.Error.Message}
		}
		return nil
	case <-ctx.Done():
		forget()
		return ErrCoreKeysUnconfirmed
	case <-timer.C:
		forget()
		return ErrCoreKeysUnconfirmed
	}
}

// Close detaches and closes the connection.
func (a *CoreAttachment) Close() error {
	_ = a.writeFrame(a.channel, coreFrameClose, nil)
	return a.conn.Close()
}

// firstPaint collects a fresh attach stream's DATA until the paint's end
// marker, which may be split across frames, and keeps only the paint.
type firstPaint struct{ paint []byte }

func (p *firstPaint) add(data []byte) (bool, error) {
	searchFrom := max(0, len(p.paint)-len(corePaintEndMarkers[0])+1)
	p.paint = append(p.paint, data...)
	if end := CorePaintEnd(p.paint[searchFrom:]); end >= 0 {
		p.paint = p.paint[:searchFrom+end]
		return true, nil
	}
	if len(p.paint) > CoreMaxPaintBytes {
		return false, errors.New("Herdr Core terminal history exceeded the reader limit")
	}
	return false, nil
}

// ReadFirstPaint attaches as an observer with history lines, keeps the first
// paint and detaches. It never resizes the terminal. The wait is bounded by
// the request timeout.
func (c *CoreClient) ReadFirstPaint(ctx context.Context, address string, history int) (paint []byte, cols, rows int, err error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	attachment, err := c.Attach(ctx, address, 80, 24, true, false, history)
	if err != nil {
		return nil, 0, 0, err
	}
	stop := context.AfterFunc(ctx, func() { _ = attachment.conn.Close() })
	defer stop()
	defer attachment.Close()
	var collected firstPaint
	errDone := errors.New("first paint complete")
	runErr := attachment.Run(func(event CoreStreamEvent) error {
		if event.Closed {
			return fmt.Errorf("Herdr Core ended the stream: %s", event.Reason)
		}
		done, err := collected.add(event.Data)
		if done {
			return errDone
		}
		if err != nil {
			return err
		}
		return attachment.CreditStream(len(event.Data))
	})
	if errors.Is(runErr, errDone) {
		return collected.paint, attachment.Cols, attachment.Rows, nil
	}
	if ctx.Err() != nil {
		return nil, 0, 0, fmt.Errorf("Herdr Core sent no complete first paint in time: %w", ctx.Err())
	}
	return nil, 0, 0, runErr
}

var (
	corePaintOSC     = regexp.MustCompile("\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")
	corePaintClear   = regexp.MustCompile("\n*\x1b\\[H\x1b\\[2J")
	corePaintCharset = regexp.MustCompile("\x1b[()*+].")
)

// CoreReaderANSI turns a first paint into Reader's history text. The paint is
// rebuilt from screen cells, so its clears come only from its own layout:
// history, blank lines that push it into scrollback, a clear, then the
// screen. Reader shows history and screen as one list, so the push-up and
// the clears go. Titles, status reports and character set choices are not
// output and go too.
func CoreReaderANSI(paint []byte) []byte {
	return corePaintCharset.ReplaceAll(corePaintClear.ReplaceAll(corePaintOSC.ReplaceAll(paint, nil), nil), nil)
}
