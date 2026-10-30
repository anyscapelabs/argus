package Protocol

import (
	"bufio"
	"io"
	"strings"
)

const (
	maxLineLength = 1 << 20
	maxFrameSize  = 8 << 20
)

type Frame struct {
	Event string
	Data  string
}

const DataDone = "[DONE]"

type FrameReader struct {
	sc    *bufio.Scanner
	event string
	data  []string
	size  int
}

func NewFrames(r io.Reader) *FrameReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxLineLength)
	return &FrameReader{sc: sc}
}

func (f *FrameReader) Next() (Frame, error) {
	for f.sc.Scan() {
		if f.sc.Err() != nil {
			return Frame{}, f.sc.Err()
		}
		line := strings.TrimSuffix(f.sc.Text(), "\r")
		switch {
		case line == "":
			frame, ok := f.take()
			if ok {
				return frame, nil
			}
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event:"):
			f.event = field(line)
		case strings.HasPrefix(line, "data:"):
			v := line[5:]
			v = strings.TrimPrefix(v, " ")
			f.data = append(f.data, v)
			f.size += len(v)
			if f.size > maxFrameSize {
				return Frame{}, io.ErrShortBuffer
			}
		default:
		}
	}
	if err := f.sc.Err(); err != nil {
		return Frame{}, err
	}
	frame, ok := f.take()
	if ok {
		return frame, nil
	}
	return Frame{}, io.EOF
}

func (f *FrameReader) take() (Frame, bool) {
	if len(f.data) == 0 {
		f.event = ""
		return Frame{}, false
	}
	frame := Frame{Event: f.event, Data: strings.Join(f.data, "\n")}
	f.event = ""
	f.data = f.data[:0]
	f.size = 0
	return frame, true
}

func field(line string) string {
	v := line[len("event:"):]
	return strings.TrimSpace(v)
}
