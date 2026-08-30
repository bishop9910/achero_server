package audio

import (
	"encoding/binary"
	"fmt"
	"os"
)

// MPEG-1 Layer III bitrate table (kbps), indexed by the frame header bitrate
// index (0-15).
var mpeg1L3Bitrates = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}

// MPEG-2 / MPEG-2.5 Layer III bitrate table (kbps).
var mpeg2L3Bitrates = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}

var mpeg1SampleRates = [3]int{44100, 48000, 32000}
var mpeg2SampleRates = [3]int{22050, 24000, 16000}
var mpeg25SampleRates = [3]int{11025, 12000, 8000}

// mp3Duration estimates MP3 duration. It prefers the Xing/Info VBR frame count
// when present and falls back to a CBR estimate from the file size.
func mp3Duration(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	fileSize := st.Size()

	// Skip an ID3v2 tag at the front when present.
	offset := int64(0)
	var head [10]byte
	if _, err := f.ReadAt(head[:], 0); err == nil && string(head[0:3]) == "ID3" {
		size := int64(head[6])<<21 | int64(head[7])<<14 | int64(head[8])<<7 | int64(head[9])
		offset = 10 + size
	}

	var hdr [4]byte
	if _, err := f.ReadAt(hdr[:], offset); err != nil {
		return 0, err
	}
	if hdr[0] != 0xFF || hdr[1]&0xE0 != 0xE0 {
		return 0, fmt.Errorf("not an mp3 frame")
	}

	version := (hdr[1] >> 3) & 0x3
	layer := (hdr[1] >> 1) & 0x3
	if layer != 0x1 {
		return 0, fmt.Errorf("not layer III")
	}
	bitrateIdx := int(hdr[2] >> 4)
	samplerateIdx := int(hdr[2]>>2) & 0x3
	padding := int(hdr[2]>>1) & 0x1

	var bitrate, sampleRate, samplesPerFrame int
	switch version {
	case 3: // MPEG-1
		bitrate = mpeg1L3Bitrates[bitrateIdx]
		sampleRate = mpeg1SampleRates[samplerateIdx]
		samplesPerFrame = 1152
	case 2: // MPEG-2
		bitrate = mpeg2L3Bitrates[bitrateIdx]
		sampleRate = mpeg2SampleRates[samplerateIdx]
		samplesPerFrame = 576
	case 0: // MPEG-2.5
		bitrate = mpeg2L3Bitrates[bitrateIdx]
		sampleRate = mpeg25SampleRates[samplerateIdx]
		samplesPerFrame = 576
	default:
		return 0, fmt.Errorf("reserved mpeg version")
	}
	if bitrate == 0 || sampleRate == 0 {
		return 0, fmt.Errorf("invalid bitrate/sample rate")
	}

	frameLen := 144*bitrate*1000/sampleRate + padding

	// Xing/Info VBR header carries the exact frame count.
	if frames, ok := mp3XingFrames(f, offset, int64(frameLen)); ok {
		return int64(frames) * int64(samplesPerFrame) * 1000 / int64(sampleRate), nil
	}

	// CBR estimate: ms = bytes * 8 / bitrate(kbps).
	durationMs := (fileSize - offset) * 8 / int64(bitrate)
	if durationMs < 0 {
		durationMs = 0
	}
	return durationMs, nil
}

// mp3XingFrames looks for a Xing/Info header inside the first frame and returns
// the encoded frame count.
func mp3XingFrames(f *os.File, offset int64, frameLen int64) (uint32, bool) {
	if frameLen <= 0 || frameLen > 4096 {
		return 0, false
	}
	buf := make([]byte, frameLen+4)
	n, err := f.ReadAt(buf, offset)
	if err != nil || n < 4 {
		return 0, false
	}
	limit := n
	if limit > 512 {
		limit = 512
	}
	for i := 4; i+12 < limit; i++ {
		if (buf[i] == 'X' || buf[i] == 'I') && buf[i+1] == 'i' && buf[i+2] == 'n' && buf[i+3] == 'g' {
			flags := binary.BigEndian.Uint32(buf[i+4 : i+8])
			if flags&0x1 != 0 {
				return binary.BigEndian.Uint32(buf[i+8 : i+12]), true
			}
			return 0, false
		}
	}
	return 0, false
}
