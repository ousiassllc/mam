package facesim

import (
	"fmt"
	"math"
	"sort"
)

// detection is a face found by the detector, in the pixel coordinates of the detector input.
type detection struct {
	x1, y1, x2, y2 float32
	score          float32
	// landmarks are right eye, left eye, nose tip, right and left mouth corner (image coordinates).
	landmarks [5][2]float32
}

// yunetStrides are the strides of the three YuNet output scales.
var yunetStrides = [...]int{8, 16, 32}

// yunetOutputs are the tensors of one input, by stride.
type yunetOutputs struct {
	cls, obj, bbox, kps map[int][]float32
}

// decodeYuNet turns the raw outputs of YuNet (input size×size, letterboxed) into detections above
// minScore, after non-maximum suppression at nmsIoU. The decoding follows OpenCV's FaceDetectorYN.
func decodeYuNet(out yunetOutputs, size int, minScore, nmsIoU float32) ([]detection, error) {
	var found []detection
	for _, s := range yunetStrides {
		n := (size / s) * (size / s)
		cls, obj, bbox, kps := out.cls[s], out.obj[s], out.bbox[s], out.kps[s]
		if len(cls) != n || len(obj) != n || len(bbox) != n*4 || len(kps) != n*10 {
			return nil, fmt.Errorf("facesim: unexpected YuNet output sizes at stride %d", s)
		}
		cols := size / s
		for idx := range n {
			score := float32(math.Sqrt(float64(clamp01(cls[idx]) * clamp01(obj[idx]))))
			if score < minScore {
				continue
			}
			r, c := idx/cols, idx%cols
			fs := float32(s)
			cx, cy := (float32(c)+bbox[idx*4])*fs, (float32(r)+bbox[idx*4+1])*fs
			w, h := float32(math.Exp(float64(bbox[idx*4+2])))*fs, float32(math.Exp(float64(bbox[idx*4+3])))*fs
			d := detection{x1: cx - w/2, y1: cy - h/2, x2: cx + w/2, y2: cy + h/2, score: score}
			for k := range 5 {
				d.landmarks[k] = [2]float32{(kps[idx*10+2*k] + float32(c)) * fs, (kps[idx*10+2*k+1] + float32(r)) * fs}
			}
			found = append(found, d)
		}
	}
	return nms(found, nmsIoU), nil
}

func clamp01(v float32) float32 { return min(max(v, 0), 1) }

// nms keeps the best-scoring detections that overlap no better one by more than iou.
func nms(ds []detection, iou float32) []detection {
	sort.Slice(ds, func(i, j int) bool { return ds[i].score > ds[j].score })
	var kept []detection
	for _, d := range ds {
		overlaps := false
		for _, k := range kept {
			if overlap(d, k) > iou {
				overlaps = true
				break
			}
		}
		if !overlaps {
			kept = append(kept, d)
		}
	}
	return kept
}

func overlap(a, b detection) float32 {
	iw := min(a.x2, b.x2) - max(a.x1, b.x1)
	ih := min(a.y2, b.y2) - max(a.y1, b.y1)
	if iw <= 0 || ih <= 0 {
		return 0
	}
	inter := iw * ih
	union := (a.x2-a.x1)*(a.y2-a.y1) + (b.x2-b.x1)*(b.y2-b.y1) - inter
	if union <= 0 {
		return 0
	}
	return inter / union
}
