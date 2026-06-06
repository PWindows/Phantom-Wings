package progress

import (
	"io"
	"strings"
	"sync/atomic"

	"github.com/pwindows/phantom-wings/system"
)

type Progress struct {
	written uint64
	total   uint64
	Writer  io.Writer
}

func NewProgress(total uint64) *Progress {
	return &Progress{total: total}
}

func (p *Progress) Written() uint64 {
	return atomic.LoadUint64(&p.written)
}

func (p *Progress) Total() uint64 {
	return atomic.LoadUint64(&p.total)
}

func (p *Progress) SetTotal(total uint64) {
	atomic.StoreUint64(&p.total, total)
}

func (p *Progress) Write(v []byte) (int, error) {
	n := len(v)
	atomic.AddUint64(&p.written, uint64(n))
	if p.Writer != nil {
		return p.Writer.Write(v)
	}
	return n, nil
}

func (p *Progress) Progress(width int) string {
	current := p.Written()
	total := p.Total()
	widthPercentage := float64(100) / float64(width)
	percentageDecimal := float64(current) / float64(total)
	percentage := percentageDecimal * 100
	ticks := int(percentage / widthPercentage)

	if ticks < 0 {
		ticks = 0
	} else if ticks > width {
		ticks = width
	}

	bar := strings.Repeat("=", ticks) + strings.Repeat(" ", width-ticks)
	return "[" + bar + "] " + system.FormatBytes(current) + " / " + system.FormatBytes(total)
}