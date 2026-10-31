package herdr

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

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
	}{
		{name: "idle", minFrames: 2, paintEnds: "\x1b[?9001l", readerHas: []string{"idle 001", "idle 250"}, readerLast: "idle 400"},
		{name: "busy", minFrames: 2, paintEnds: "\x1b[?9001l", liveAfter: "\x1b[32mlive ", readerHas: []string{"live 00"}},
		{name: "marker", minFrames: 1, paintEnds: "\x1b[?9001h\x1b]7501;state=idle", readerHas: []string{"text [?9001l and [?9001h line 01"}, readerLast: "line 30"},
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
		})
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
