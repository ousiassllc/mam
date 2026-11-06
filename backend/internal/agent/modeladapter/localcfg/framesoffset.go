package localcfg

import "fmt"

// The offset of a frames assignment (Issue #1076): the value put into the workflow is the frame count plus the offset, so that a graph that needs
// "frames-2" (the gray frames between the first and the last frame of a clip) can say so in its assignments.

// maxFramesOffset bounds Assignment.Offset: a few frames (the first and the last frame of a clip), not a second length.
const maxFramesOffset = 64

// FramesFor is the value a ParamFrames assignment puts in: the frame count of the clip with the assignment's offset (at least 1).
func (a Assignment) FramesFor(frames int) int { return max(1, frames+a.Offset) }

// validateFramesOffset checks the offset of an assignment: it belongs to frames only and stays within maxFramesOffset.
func validateFramesOffset(at string, a Assignment) []error {
	if a.Offset != 0 && (a.Param != ParamFrames || a.Offset < -maxFramesOffset || a.Offset > maxFramesOffset) {
		return []error{fmt.Errorf("%s: offset applies to frames only and lies within -%d to %d", at, maxFramesOffset, maxFramesOffset)}
	}
	return nil
}
