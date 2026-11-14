package herdr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var updateReaderText = flag.Bool("update-reader-text", false, "rewrite testdata/core-reader-*.ansi from the captured paints")

// The testdata files are the raw framed bytes a real herdr-core 0.1.15 sent
// to a fresh observing attach with history 500, right after hello:
//   - idle: 400 coloured lines, then sleep. The paint is two DATA frames.
//   - busy: the same, then a coloured line every 2 ms, toggling CSI ?9001h
//     and l itself every 50 lines. The paint's last bytes share a DATA frame
//     with live output.
//   - marker: a program that turned on win32-input-mode itself, set a title
//     naming the marker, printed the marker's visible text and reported a
//     status.
func readPaintCapture(t *testing.T, name string) (frames [][]byte) {
	t.Helper()
	raw, err := os.ReadFile("testdata/core-paint-" + name + ".bin")
	if err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(raw)
	for {
		frame, err := readCoreFrame(reader)
		if errors.Is(err, io.EOF) {
			return frames
		}
		if err != nil {
			t.Fatal(err)
		}
		if frame.channel != 0 && frame.kind == coreFrameData {
			frames = append(frames, frame.payload)
		}
	}
}

func TestFirstPaintEndsAtItsMarkerInRealCoreStreams(t *testing.T) {
	for _, test := range []struct {
		name       string
		minFrames  int
		paintEnds  string
		liveAfter  string
		readerHas  []string
		readerLast string
		row        string
		rows       int
	}{
		{name: "idle", minFrames: 2, paintEnds: "\x1b[?9001l", readerHas: []string{"idle 001", "idle 250"}, readerLast: "idle 400", row: `idle (\d+)`, rows: 400},
		{name: "busy", minFrames: 2, paintEnds: "\x1b[?9001l", liveAfter: "\x1b[32mlive ", readerHas: []string{"live 00"}, row: `live (\d+)`, rows: 619},
		{name: "marker", minFrames: 1, paintEnds: "\x1b[?9001h\x1b]7501;state=idle", readerHas: []string{"text [?9001l and [?9001h line 01"}, readerLast: "line 30", row: `line (\d+)`, rows: 30},
	} {
		t.Run(test.name, func(t *testing.T) {
			frames := readPaintCapture(t, test.name)
			if len(frames) < test.minFrames {
				t.Fatalf("%d DATA frames, want at least %d", len(frames), test.minFrames)
			}
			var collected firstPaint
			var stream []byte
			used := -1
			for index, frame := range frames {
				stream = append(stream, frame...)
				if used < 0 {
					done, err := collected.add(frame)
					if err != nil {
						t.Fatal(err)
					}
					if done {
						used = index + 1
					}
				}
			}
			if used < test.minFrames {
				t.Fatalf("paint ended after %d frames, want at least %d", used, test.minFrames)
			}
			paint := collected.paint
			if !bytes.HasPrefix(stream, paint) || !bytes.HasSuffix(paint, []byte("\x1b[?9001l")) && !bytes.HasSuffix(paint, []byte("\x1b[?9001h")) {
				t.Fatalf("paint is not a prefix of the stream ending at its marker: %q", tail(paint))
			}
			if !strings.Contains(string(stream[max(0, len(paint)-40):]), test.paintEnds) {
				t.Fatalf("paint end %q not at the cut: %q", test.paintEnds, tail(stream[:min(len(stream), len(paint)+40)]))
			}
			if test.liveAfter != "" {
				rest := stream[len(paint):]
				if !bytes.HasPrefix(rest, []byte(test.liveAfter)) {
					t.Fatalf("live output does not follow the paint: %q", rest[:min(len(rest), 40)])
				}
				if !bytes.Contains(rest, []byte("\x1b[?9001h")) {
					t.Fatal("capture should hold the program's own marker after the paint")
				}
				usedBytes := 0
				for _, frame := range frames[:used] {
					usedBytes += len(frame)
				}
				if usedBytes == len(paint) {
					t.Fatal("capture should end the paint inside a frame that also holds live output")
				}
			}
			reader := string(CoreReaderANSI(paint))
			for _, want := range test.readerHas {
				if !strings.Contains(reader, want) {
					t.Errorf("Reader text lacks %q", want)
				}
			}
			if test.readerLast != "" && !strings.Contains(reader[max(0, len(reader)-200):], test.readerLast) {
				t.Errorf("Reader text does not end with %q: %q", test.readerLast, reader[max(0, len(reader)-200):])
			}
			for _, unwanted := range []string{"\x1b]", "\x1b[2J", "7501", "title", "\n\n\n"} {
				if strings.Contains(reader, unwanted) {
					t.Errorf("Reader text holds %q", unwanted)
				}
			}
			checkReaderGolden(t, test.name, reader)
			checkOneRowPerLine(t, reader, test.row, test.rows)
		})
	}
}

// checkOneRowPerLine splits the text where Reader starts a new line, at each
// line feed, and checks every line holds exactly one row, in order. A history
// line joined with the first screen line, or a lost line, fails.
func checkOneRowPerLine(t *testing.T, reader, row string, rows int) {
	t.Helper()
	if strings.HasPrefix(reader, "\n") {
		t.Error("Reader text starts with a line break")
	}
	pattern := regexp.MustCompile(row)
	lines := strings.Split(reader, "\n")
	if len(lines) != rows {
		t.Errorf("Reader text has %d lines, want %d", len(lines), rows)
	}
	previous := -1
	for index, line := range lines {
		found := pattern.FindAllStringSubmatch(line, -1)
		if len(found) != 1 {
			t.Fatalf("line %d holds %d rows: %q", index, len(found), line)
		}
		number, _ := strconv.Atoi(found[0][1])
		if previous >= 0 && number != previous+1 {
			t.Fatalf("line %d is row %d after row %d", index, number, previous)
		}
		previous = number
	}
}

// checkReaderGolden keeps testdata/core-reader-*.ansi equal to the Reader
// text, so the browser tests render the same text with Reader's renderer.
func checkReaderGolden(t *testing.T, name, reader string) {
	t.Helper()
	path := "testdata/core-reader-" + name + ".ansi"
	if *updateReaderText {
		if err := os.WriteFile(path, []byte(reader), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(golden) != reader {
		t.Errorf("%s differs from the Reader text; rerun with -update-reader-text", path)
	}
}

func TestFirstPaintFindsAMarkerSplitAcrossFrames(t *testing.T) {
	var collected firstPaint
	for index, part := range []string{"screen\x1b[", "?90", "01l\x1b]7501;state=idle\x1b\\live"} {
		done, err := collected.add([]byte(part))
		if err != nil || done != (index == 2) {
			t.Fatalf("part %d: done %v, %v", index, done, err)
		}
	}
	if got := string(collected.paint); got != "screen\x1b[?9001l" {
		t.Fatalf("paint = %q", got)
	}
}

func tail(value []byte) []byte { return value[max(0, len(value)-60):] }

func TestReaderTextDropsCharacterSetChoices(t *testing.T) {
	// Claude Code's paint ends a line with ESC ( B, which Reader would show
	// as a stray B.
	got := string(CoreReaderANSI([]byte("Esc to cancel\x1b[0m\x1b[11;2H\x1b(B\x1b[0m")))
	if got != "Esc to cancel\x1b[0m\x1b[11;2H\x1b[0m" {
		t.Fatalf("Reader text = %q", got)
	}
}

// Core 0.1.21 paints a terminal on the alternate screen after the hidden
// primary screen, with no line break between their rows. Older cores paint
// only the alternate screen, and Reader keeps it as it was.
func TestReaderTextStartsTheAlternateScreenOnItsOwnLine(t *testing.T) {
	const reset = "\x18\x1b[!p\x1b[?2004l\x1b[?1049l\x1b[?1047l\x1b[?47l"
	for _, test := range []struct{ paint, want string }{
		{reset + "primary 0199\r\nprimary 0200\x1b[40;1H\x1b[0m\x1b[?1049h\x1b[!p\x1b[HALT 0001", reset + "primary 0199\r\nprimary 0200\x1b[40;1H\x1b[0m\n\x1b[!p\x1b[HALT 0001"},
		{reset + "\x1b[?1049h\x1b[?1h\x1b[9G\x1bH\x1b[HALT 0001", reset + "\x1b[?1049h\x1b[?1h\x1b[9G\x1bH\x1b[HALT 0001"},
		{reset + "$ less\r\n  \x1b[2;1H\x1b[?1049h\x1b[!p\x1b[HALT 0001", reset + "$ less\r\n  \x1b[2;1H\x1b[?1049h\x1b[!p\x1b[HALT 0001"},
	} {
		if got := string(CoreReaderANSI([]byte(test.paint))); got != test.want {
			t.Errorf("Reader text = %q, want %q", got, test.want)
		}
	}
}

// Core answers a program's colour queries from the viewer in use. Only a
// controlling attach reports the browser's colours; an observer never does.
func TestControlAttachReportsTheBrowserColours(t *testing.T) {
	core, _ := newTestCore(t)
	path := shortSocketPath(t)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	attached := make(chan json.RawMessage, 2)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			reader := bufio.NewReader(conn)
			if _, err := reader.ReadBytes('\n'); err != nil {
				conn.Close()
				continue
			}
			_ = json.NewEncoder(conn).Encode(map[string]any{"jsonrpc": "2.0", "id": 0, "result": core.hello})
			frame, err := readCoreFrame(reader)
			var call struct {
				ID     uint64          `json:"id"`
				Params json.RawMessage `json:"params"`
			}
			if err == nil && json.Unmarshal(frame.payload, &call) == nil {
				attached <- call.Params
				refused, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.ID, "error": map[string]any{"code": -32000, "message": "not_found", "data": map[string]any{"kind": "not_found"}}})
				_, _ = conn.Write(encodeCoreFrame(0, coreFrameData, refused))
			}
			conn.Close()
		}
	}()
	client := NewCoreClient(path, nil)
	for _, observe := range []bool{false, true} {
		if _, err := client.Attach(context.Background(), "t1", 80, 24, observe, false, 0); err == nil {
			t.Fatal("attach succeeded against a refusing core")
		}
		var params struct {
			Display map[string]string `json:"display"`
		}
		if err := json.Unmarshal(<-attached, &params); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"bg": "#101715", "fg": "#e5e7df", "cursor": "#e89b73"}
		if observe {
			want = nil
		}
		if !reflect.DeepEqual(params.Display, want) {
			t.Fatalf("observe %v: display = %v, want %v", observe, params.Display, want)
		}
	}
}
