package audio

import (
	"encoding/binary"
	"fmt"
	"os"
)

// mp4Duration walks the MP4 atom tree to the moov/mvhd atom and derives the
// duration from its timescale and duration fields.
func mp4Duration(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := st.Size()

	for pos := int64(0); pos+8 <= size; {
		atomSize, typ, headerLen, err := readAtom(f, pos, size)
		if err != nil {
			return 0, err
		}
		if typ == "moov" {
			return mp4MoovDuration(f, pos+headerLen, pos+atomSize)
		}
		pos += atomSize
	}
	return 0, fmt.Errorf("moov atom not found")
}

func mp4MoovDuration(f *os.File, start, end int64) (int64, error) {
	for pos := start; pos+8 <= end; {
		atomSize, typ, headerLen, err := readAtom(f, pos, end)
		if err != nil {
			return 0, err
		}
		if typ == "mvhd" {
			return mp4MvhdDuration(f, pos+headerLen)
		}
		pos += atomSize
	}
	return 0, fmt.Errorf("mvhd atom not found")
}

// readAtom parses an atom header at pos and returns its total size, type and
// header length (8 or 16 for the 64-bit extended size form).
func readAtom(f *os.File, pos, limit int64) (int64, string, int64, error) {
	var hdr [8]byte
	if _, err := f.ReadAt(hdr[:], pos); err != nil {
		return 0, "", 0, err
	}
	atomSize := int64(binary.BigEndian.Uint32(hdr[0:4]))
	typ := string(hdr[4:8])
	headerLen := int64(8)
	switch atomSize {
	case 1:
		var ext [8]byte
		if _, err := f.ReadAt(ext[:], pos+8); err != nil {
			return 0, "", 0, err
		}
		atomSize = int64(binary.BigEndian.Uint64(ext[:]))
		headerLen = 16
	case 0:
		atomSize = limit - pos
	}
	if atomSize < headerLen {
		return 0, "", 0, fmt.Errorf("bad atom size")
	}
	return atomSize, typ, headerLen, nil
}

func mp4MvhdDuration(f *os.File, payload int64) (int64, error) {
	var ver [4]byte
	if _, err := f.ReadAt(ver[:], payload); err != nil {
		return 0, err
	}
	if ver[0] == 1 {
		var ts [4]byte
		var dur [8]byte
		if _, err := f.ReadAt(ts[:], payload+20); err != nil {
			return 0, err
		}
		if _, err := f.ReadAt(dur[:], payload+24); err != nil {
			return 0, err
		}
		timescale := binary.BigEndian.Uint32(ts[:])
		duration := binary.BigEndian.Uint64(dur[:])
		if timescale == 0 {
			return 0, fmt.Errorf("invalid timescale")
		}
		return int64(duration) * 1000 / int64(timescale), nil
	}
	var ts [4]byte
	var dur [4]byte
	if _, err := f.ReadAt(ts[:], payload+12); err != nil {
		return 0, err
	}
	if _, err := f.ReadAt(dur[:], payload+16); err != nil {
		return 0, err
	}
	timescale := binary.BigEndian.Uint32(ts[:])
	duration := binary.BigEndian.Uint32(dur[:])
	if timescale == 0 {
		return 0, fmt.Errorf("invalid timescale")
	}
	return int64(duration) * 1000 / int64(timescale), nil
}
