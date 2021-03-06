package compress

import (
	"io"

	"github.com/pierrec/lz4"
)

type LZ4Maker struct{ Level }

func (l LZ4Maker) Make() Compressor {
	return &LZ4{lz4.NewWriter(nil), new(WCounter), l.Level}
}

// LZ4 is a wrapper for lz4.Writer which knows how to Flush properly.
type LZ4 struct {
	*lz4.Writer
	ct *WCounter
	Level
}

func (l *LZ4) Count() int64 {
	return l.ct.Count()
}

// Flush flushes and finalizes the LZ4 block stream.
func (l *LZ4) Flush() error { return l.Writer.Flush() }
func (l *LZ4) Close() error { return l.Writer.Close() }
func (l *LZ4) Reset(w io.Writer) {
	l.ct.Written = 0
	l.ct.Writer = w

	l.Writer.Reset(l.ct)
	l.Writer.Header = lz4.Header{
		CompressionLevel: func() int {
			switch l.Level {
			case Fastest:
				return 0
			case Medium:
				return 3
			case High:
				return 5
			case LZ4HC:
				return 9
			default:
				return 3
			}
		}(),
	}
}
