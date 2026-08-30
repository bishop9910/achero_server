package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// oggDuration estimates Ogg duration from the last page's granule position.
// Opus is always 48 kHz; Vorbis sample rate comes from the identification
// header.
func oggDuration(path, ext string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	sampleRate := int64(48000)
	if ext == ".ogg" {
		sr, err := oggVorbisSampleRate(f)
		if err != nil {
			return 0, err
		}
		sampleRate = sr
	}

	granule, err := oggLastGranule(f)
	if err != nil {
		return 0, err
	}
	if sampleRate <= 0 {
		return 0, fmt.Errorf("invalid sample rate")
	}
	return granule * 1000 / sampleRate, nil
}

// oggVorbisSampleRate reads the first page's Vorbis identification header.
func oggVorbisSampleRate(f *os.File) (int64, error) {
	var page [27]byte
	if _, err := io.ReadFull(f, page[:]); err != nil {
		return 0, err
	}
	if string(page[0:4]) != "OggS" {
		return 0, fmt.Errorf("not ogg")
	}
	segCount := int(page[26])
	segTable := make([]byte, segCount)
	if _, err := io.ReadFull(f, segTable); err != nil {
		return 0, err
	}
	// The first packet ends at the first lacing value < 255.
	var pkt []byte
	for _, l := range segTable {
		b := make([]byte, l)
		if _, err := io.ReadFull(f, b); err != nil {
			return 0, err
		}
		pkt = append(pkt, b...)
		if l < 255 {
			break
		}
	}
	if len(pkt) < 16 || pkt[0] != 1 || string(pkt[1:7]) != "vorbis" {
		return 0, fmt.Errorf("not a vorbis identification header")
	}
	// version(4 LE) + channels(1) + sample rate(4 LE) at offset 12.
	return int64(binary.LittleEndian.Uint32(pkt[12:16])), nil
}

// oggLastGranule reads the granule position (total samples) from the last Ogg
// page by scanning the tail of the file backwards.
func oggLastGranule(f *os.File) (int64, error) {
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := st.Size()
	const tail = 64 * 1024
	off := size - tail
	if off < 0 {
		off = 0
	}
	buf := make([]byte, size-off)
	if _, err := f.ReadAt(buf, off); err != nil {
		return 0, err
	}
	for i := len(buf) - 14; i >= 0; i-- {
		if string(buf[i:i+4]) == "OggS" {
			return int64(binary.LittleEndian.Uint64(buf[i+6 : i+14])), nil
		}
	}
	return 0, fmt.Errorf("no ogg page found")
}
