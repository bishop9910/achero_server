package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// wavDuration derives duration from the fmt chunk byte rate and the data chunk
// size.
func wavDuration(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var riff [12]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return 0, err
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return 0, fmt.Errorf("not wav")
	}

	var byteRate, dataSize uint32
	var haveFmt, haveData bool
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(f, hdr[:]); err != nil {
			break
		}
		chunkSize := binary.LittleEndian.Uint32(hdr[4:8])
		switch string(hdr[0:4]) {
		case "fmt ":
			var fmt [16]byte
			if _, err := io.ReadFull(f, fmt[:]); err != nil {
				return 0, err
			}
			byteRate = binary.LittleEndian.Uint32(fmt[8:12])
			haveFmt = true
			if skip := int64(chunkSize) - 16; skip > 0 {
				if _, err := f.Seek(skip, io.SeekCurrent); err != nil {
					return 0, err
				}
			}
		case "data":
			dataSize = chunkSize
			haveData = true
			if _, err := f.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				return 0, err
			}
		default:
			if _, err := f.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				return 0, err
			}
		}
		if chunkSize%2 == 1 {
			if _, err := f.Seek(1, io.SeekCurrent); err != nil {
				return 0, err
			}
		}
	}
	if !haveFmt || byteRate == 0 || !haveData {
		return 0, fmt.Errorf("incomplete wav")
	}
	return int64(dataSize) * 1000 / int64(byteRate), nil
}
