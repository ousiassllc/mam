package ffmpegdl

// HoldRemoval makes Remove call hold in place of the deletion of the files, so that a test can keep a removal in the middle.
func HoldRemoval(s *Service, hold func() error) { s.removeFiles = hold }
