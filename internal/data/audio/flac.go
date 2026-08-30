package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// flacDuration reads the STREAMINFO block of a FLAC file and returns the
// duration in milliseconds.
func flacDuration(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return 0, err
	}
	if string(magic[:]) != "fLaC" {
		return 0, fmt.Errorf("not flac")
	}

	var hdr [4]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return 0, err
	}
	if blockType := hdr[0] & 0x7F; blockType != 0 {
		return 0, fmt.Errorf("first block is not STREAMINFO")
	}

	var si [34]byte
	if _, err := io.ReadFull(f, si[:]); err != nil {
		return 0, err
	}

	// Bytes 10..17 pack: sample rate (20 bits), channels-1 (3), bps-1 (5),
	// total samples (36 bits).
	u := binary.BigEndian.Uint64(si[10:18])
	sampleRate := u >> 44
	totalSamples := u & 0xFFFFFFFFF
	if sampleRate == 0 {
		return 0, fmt.Errorf("invalid sample rate")
	}
	return int64(totalSamples) * 1000 / int64(sampleRate), nil
}
