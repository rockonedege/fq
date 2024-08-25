package midi

// https://www.midi.org/specifications/item/the-midi-1-0-specification

import (
	"bytes"
	"embed"
	"encoding/binary"

	"github.com/wader/fq/format"
	"github.com/wader/fq/pkg/decode"
	"github.com/wader/fq/pkg/interp"
)

type context struct {
	tick    uint64
	running uint8
	casio   bool
}

//go:embed midi.md
var midiFS embed.FS

func init() {
	interp.RegisterFormat(
		format.MIDI,
		&decode.Format{
			Description: "Standard MIDI file",
			DecodeFn:    decodeMIDI,
		})

	interp.RegisterFS(midiFS)
}

func decodeMIDI(d *decode.D) any {
	d.Endian = decode.BigEndian

	// ... skip to MThd chunk
	for d.BitsLeft() > 0 {
		if tag, len, err := peekChunk(d); err != nil {
			d.Errorf("error reading MIDI file chunk (%v)", err)
		} else if tag == "MThd" {
			break
		} else {
			d.SeekRel(8 * int64(len+8))
		}
	}

	// ... read MThd chunk
	if d.BitsLeft() < 64 {
		d.Errorf("missing MThd header chunk")
	} else {
		d.FieldStruct("header", decodeMThd)

		if d.BitsLeft() < 64 {
			d.Errorf("missing MTrk track chunk(s)")
		} else {
			// ... read MTrk chunks, discarding unexpected chunks
			d.FieldArray("tracks", func(d *decode.D) {
				for d.BitsLeft() > 0 {
					for d.BitsLeft() > 0 {
						if tag, len, err := peekChunk(d); err != nil {
							d.Errorf("error reading MIDI file chunk (%v)", err)
						} else if tag == "MTrk" {
							d.FieldStruct("track", decodeMTrk)
							break
						} else {
							d.SeekRel(8 * int64(len+8))
						}
					}
				}
			})
		}
	}

	return nil
}

func decodeMThd(d *decode.D) {
	d.AssertLeastBytesLeft(8)

	if !bytes.Equal(d.PeekBytes(4), []byte("MThd")) {
		d.Errorf("no MThd marker")
	}

	d.FieldArray("header", func(d *decode.D) {
		d.FieldUTF8("tag", 4)
		length := d.FieldS32("length")

		d.AssertLeastBytesLeft(length)

		d.FramedFn(length*8, func(d *decode.D) {
			format := d.FieldU16("format")
			if format != 0 && format != 1 && format != 2 {
				d.Errorf("invalid MThd format %v (expected 0,1 or 2)", format)
			}

			tracks := d.FieldU16("tracks")
			if format == 0 && tracks > 1 {
				d.Errorf("MIDI format 0 expects 1 track (got %v)", tracks)
			}

			division := d.FieldU16("divisions")
			if division&0x8000 == 0x8000 {
				SMPTE := (division & 0xff00) >> 8
				if SMPTE != 0xe8 && SMPTE != SMPTE && SMPTE != 0xe6 && SMPTE != 0xe5 {
					d.Errorf("invalid MThd division SMPTE timecode type %02X (expected E8,E7, E6 or E5)", SMPTE)
				}
			}
		})
	})

	return
}

func decodeMTrk(d *decode.D) {
	d.AssertLeastBytesLeft(8)

	if !bytes.Equal(d.PeekBytes(4), []byte("MTrk")) {
		d.Errorf("no MTrk marker")
	}

	d.FieldUTF8("tag", 4)
	length := d.FieldS32("length")

	d.AssertLeastBytesLeft(length)

	d.FieldArray("events", func(d *decode.D) {
		d.FramedFn(length*8, func(d *decode.D) {
			ctx := context{
				tick:    0,
				running: 0x000,
				casio:   false,
			}

			for d.BitsLeft() > 0 {
				decodeEvent(d, &ctx)
			}
		})
	})
}

func decodeEvent(d *decode.D, ctx *context) {
	_, status, event := peekEvent(d)

	if status == 0xf0 || status == 0xf7 {
		decodeSysExEvent(d, status, ctx)
	} else if status == 0xff {
		decodeMetaEvent(d, event, ctx)
	} else {
		decodeMIDIEvent(d, status, ctx)
	}
}

func peekChunk(d *decode.D) (string, uint32, error) {
	if d.BitsLeft() > 64 {
		d.AssertLeastBytesLeft(8)

		bytes := d.PeekBytes(8)
		tag := string(bytes[0:4])
		len := binary.BigEndian.Uint32(bytes[4:])

		return tag, len, nil
	}

	return "", 0, nil
}

func peekEvent(d *decode.D) (uint64, uint8, uint8) {
	delta := uint64(0)
	N := 3

	for {
		bytes := d.PeekBytes(N)

		// ... peek at delta value
		delta = 0

		for i, b := range bytes[:N-2] {
			delta <<= 7
			delta += uint64(b & 0x7f)

			if b&0x80 == 0 {
				status := bytes[i+1]
				event := bytes[i+2]
				return delta, status, event
			}
		}

		N++
	}
}

func vlq(d *decode.D) uint64 {
	vlq := uint64(0)

	for {
		b := d.BytesLen(1)

		vlq <<= 7
		vlq += uint64(b[0] & 0x7f)

		if b[0]&0x80 == 0 {
			break
		}
	}

	return vlq
}

func vlf(d *decode.D) []uint8 {
	N := int(vlq(d))

	return d.BytesLen(N)
}

func flush(d *decode.D, format string, args ...any) {
	d.Errorf(format, args...)

	var N int = int(d.BitsLeft())

	d.Bits(N)
}
