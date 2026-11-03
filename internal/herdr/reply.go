package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxReplyBytes bounds one reply or event from a Herdr socket or herdr-app.
// A complete read of a large real Home is well under 1 MiB, so this only
// stops a reply that would otherwise grow Shepherdr's memory without limit.
const MaxReplyBytes = 16 << 20

// ErrReplyTooLarge is a Herdr reply over MaxReplyBytes. Shepherdr refuses it
// rather than reading part of it.
var ErrReplyTooLarge = fmt.Errorf("Herdr sent a reply larger than %d MiB", MaxReplyBytes>>20)

// replyDecoder decodes one JSON value at a time from a Herdr connection,
// refusing any value that needs more than MaxReplyBytes read for it.
type replyDecoder struct {
	limit   *replyLimit
	decoder *json.Decoder
}

type replyLimit struct {
	reader io.Reader
	left   int64
}

func (limit *replyLimit) Read(buffer []byte) (int, error) {
	if limit.left <= 0 {
		return 0, ErrReplyTooLarge
	}
	if int64(len(buffer)) > limit.left {
		buffer = buffer[:limit.left]
	}
	count, err := limit.reader.Read(buffer)
	limit.left -= int64(count)
	return count, err
}

func newReplyDecoder(reader io.Reader) *replyDecoder {
	limit := &replyLimit{reader: reader}
	return &replyDecoder{limit: limit, decoder: json.NewDecoder(limit)}
}

// Decode reads the next value. After ErrReplyTooLarge the connection is
// unusable, and every caller closes it.
func (d *replyDecoder) Decode(value any) error {
	d.limit.left = MaxReplyBytes
	err := d.decoder.Decode(value)
	if errors.Is(err, ErrReplyTooLarge) {
		return ErrReplyTooLarge
	}
	return err
}

// boundedOutput collects a command's output up to MaxReplyBytes. Past that
// it keeps accepting and dropping output, so the command can finish, and
// remembers that the reply was too large.
type boundedOutput struct {
	data     []byte
	tooLarge bool
}

func (output *boundedOutput) Write(data []byte) (int, error) {
	if output.tooLarge || len(output.data)+len(data) > MaxReplyBytes {
		output.tooLarge = true
		output.data = nil
	} else {
		output.data = append(output.data, data...)
	}
	return len(data), nil
}
