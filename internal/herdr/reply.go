package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// DefaultReplyLimit bounds one reply or event from a Herdr socket or
// herdr-app unless -herdr-reply-limit sets another. A complete Herdr TUI read
// takes about 1 KiB per workspace and terminal, so real Homes stay far below
// it; it stops a reply that would otherwise grow Shepherdr's memory without
// limit.
const DefaultReplyLimit = 64 << 20

// replyLimit is set once at startup, before any Herdr connection opens.
var replyLimit int64 = DefaultReplyLimit

// SetReplyLimit sets the reply bound. Call it before any client starts.
func SetReplyLimit(limit int64) { replyLimit = limit }

// ErrReplyTooLarge is a Herdr reply over the reply limit. Shepherdr refuses
// it rather than reading part of it.
var ErrReplyTooLarge = errors.New("Herdr sent a reply larger than the reply limit")

type replyTooLarge struct{ limit int64 }

func (err replyTooLarge) Error() string {
	return "Herdr sent a reply larger than " + formatBytes(err.limit)
}

func (replyTooLarge) Is(target error) bool { return target == ErrReplyTooLarge }

func formatBytes(size int64) string {
	switch {
	case size%(1<<30) == 0:
		return fmt.Sprintf("%d GiB", size>>30)
	case size%(1<<20) == 0:
		return fmt.Sprintf("%d MiB", size>>20)
	case size%(1<<10) == 0:
		return fmt.Sprintf("%d KiB", size>>10)
	default:
		return fmt.Sprintf("%d bytes", size)
	}
}

// replyDecoder decodes one JSON value at a time from a Herdr connection,
// refusing any value that needs more than the reply limit read for it.
type replyDecoder struct {
	limit   *replyBound
	decoder *json.Decoder
}

type replyBound struct {
	reader io.Reader
	left   int64
}

func (limit *replyBound) Read(buffer []byte) (int, error) {
	if limit.left <= 0 {
		return 0, replyTooLarge{replyLimit}
	}
	if int64(len(buffer)) > limit.left {
		buffer = buffer[:limit.left]
	}
	count, err := limit.reader.Read(buffer)
	limit.left -= int64(count)
	return count, err
}

func newReplyDecoder(reader io.Reader) *replyDecoder {
	limit := &replyBound{reader: reader}
	return &replyDecoder{limit: limit, decoder: json.NewDecoder(limit)}
}

// Decode reads the next value. After ErrReplyTooLarge the connection is
// unusable, and every caller closes it.
func (d *replyDecoder) Decode(value any) error {
	d.limit.left = replyLimit
	err := d.decoder.Decode(value)
	if errors.Is(err, ErrReplyTooLarge) {
		return replyTooLarge{replyLimit}
	}
	return err
}

// boundedOutput collects a command's output up to the reply limit. Past that
// it keeps accepting and dropping output, so the command can finish, and
// remembers that the reply was too large.
type boundedOutput struct {
	data     []byte
	tooLarge bool
}

func (output *boundedOutput) Write(data []byte) (int, error) {
	if output.tooLarge || int64(len(output.data)+len(data)) > replyLimit {
		output.tooLarge = true
		output.data = nil
	} else {
		output.data = append(output.data, data...)
	}
	return len(data), nil
}
