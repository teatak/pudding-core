// Package resample provides lightweight PCM16 resampling helpers.
package resample

type Linear struct {
	srcRate int
	dstRate int
	ratio   float64
	pos     float64
	last    int16
	have    bool
}

func NewLinear(srcRate, dstRate int) *Linear {
	if srcRate <= 0 || dstRate <= 0 {
		return nil
	}
	return &Linear{
		srcRate: srcRate,
		dstRate: dstRate,
		ratio:   float64(srcRate) / float64(dstRate),
	}
}

func (l *Linear) SrcRate() int { return l.srcRate }
func (l *Linear) DstRate() int { return l.dstRate }

func (l *Linear) Reset() {
	l.pos = 0
	l.last = 0
	l.have = false
}

func (l *Linear) Process(src []byte) []byte {
	if len(src) < 2 {
		return nil
	}
	if l.srcRate == l.dstRate {
		out := make([]byte, len(src))
		copy(out, src)
		return out
	}
	nIn := len(src) / 2
	workLen := nIn
	workOff := 0
	if l.have {
		workLen++
		workOff = 1
	}
	work := make([]int16, workLen)
	if l.have {
		work[0] = l.last
	}
	for i := 0; i < nIn; i++ {
		work[workOff+i] = int16(uint16(src[i*2]) | uint16(src[i*2+1])<<8)
	}

	estOut := int(float64(nIn)/l.ratio) + 2
	out := make([]byte, 0, estOut*2)
	pos := l.pos
	for {
		wPos := float64(workOff) + pos
		if wPos < 0 {
			pos += l.ratio
			continue
		}
		widx := int(wPos)
		if widx+1 >= workLen {
			break
		}
		frac := wPos - float64(widx)
		s1 := work[widx]
		s2 := work[widx+1]
		s := float64(s1)*(1.0-frac) + float64(s2)*frac
		si := int16(s)
		out = append(out, byte(si), byte(si>>8))
		pos += l.ratio
	}

	l.pos = pos - float64(nIn)
	l.last = work[workLen-1]
	l.have = true
	return out
}
