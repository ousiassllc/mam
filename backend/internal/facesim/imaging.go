package facesim

import (
	"image"
	"math"
)

// rgb is a float RGB raster with values in 0..255, row-major, three floats per pixel.
type rgb struct {
	w, h int
	// step is how many pixels of the source image one pixel of the raster stands for.
	step int
	pix  []float32
}

// toRGB copies img into an rgb raster. An image larger than maxSide is first reduced by whole-number
// box averaging, which keeps the later bilinear resize free of aliasing.
func toRGB(img image.Image, maxSide int) rgb {
	b := img.Bounds()
	step := 1
	if m := max(b.Dx(), b.Dy()); maxSide > 0 && m > maxSide {
		step = (m + maxSide - 1) / maxSide
	}
	w, h := (b.Dx()+step-1)/step, (b.Dy()+step-1)/step
	out := rgb{w: w, h: h, step: step, pix: make([]float32, w*h*3)}
	for y := range h {
		for x := range w {
			var r, g, bl, n float64
			for dy := range step {
				for dx := range step {
					px, py := b.Min.X+x*step+dx, b.Min.Y+y*step+dy
					if px >= b.Max.X || py >= b.Max.Y {
						continue
					}
					cr, cg, cb, _ := img.At(px, py).RGBA()
					r, g, bl, n = r+float64(cr), g+float64(cg), bl+float64(cb), n+1
				}
			}
			o := (y*w + x) * 3
			out.pix[o], out.pix[o+1], out.pix[o+2] = float32(r/n/257), float32(g/n/257), float32(bl/n/257)
		}
	}
	return out
}

// sample reads the bilinear value of channel c at (x, y); positions outside the raster read 0.
func (m rgb) sample(x, y float64, c int) float32 {
	if x < -0.5 || y < -0.5 || x > float64(m.w)-0.5 || y > float64(m.h)-0.5 {
		return 0
	}
	x0, y0 := math.Floor(x), math.Floor(y)
	fx, fy := float32(x-x0), float32(y-y0)
	at := func(px, py int) float32 {
		px, py = min(max(px, 0), m.w-1), min(max(py, 0), m.h-1)
		return m.pix[(py*m.w+px)*3+c]
	}
	ix, iy := int(x0), int(y0)
	top := at(ix, iy)*(1-fx) + at(ix+1, iy)*fx
	bottom := at(ix, iy+1)*(1-fx) + at(ix+1, iy+1)*fx
	return top*(1-fy) + bottom*fy
}
