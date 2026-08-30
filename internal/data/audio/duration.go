package audio

// duration dispatches to a format-specific parser based on the file extension.
// Unknown formats return 0 with no error; the duration field is optional in the
// protocol.
func duration(path, ext string) (int64, error) {
	switch ext {
	case ".mp3":
		return mp3Duration(path)
	case ".flac":
		return flacDuration(path)
	case ".m4a", ".m4b":
		return mp4Duration(path)
	case ".ogg", ".opus":
		return oggDuration(path, ext)
	case ".wav":
		return wavDuration(path)
	default:
		return 0, nil
	}
}
