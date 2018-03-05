package renderer

import (
	log "github.com/sirupsen/logrus"
)

type rollingLayer struct {
	queue    layersSet
	position int
}

func (rl *rollingLayer) reset() {
	rl.queue = rl.queue[:0]
}

func (rl *rollingLayer) enqueue(l *layer) {
	rl.queue = append(rl.queue, l)
}

func (rl *rollingLayer) setPos(pos int) {
	rl.position = pos
	rl.queue[0].origin.X = pos
}

func (rl *rollingLayer) advance() {
	if rl.position+displayWidth >= rl.queue[0].image.Bounds().Max.X {
		rl.setPos(displayWidth - 1 + rl.queue[0].rolling.entry)
	} else if rl.position == rl.queue[0].rolling.last && len(rl.queue) > 1 {
		log.Debug("Switch rolling layer")
		rl.setPos(0)
		rl.queue = rl.queue[1:]
	} else {
		rl.setPos(rl.position + 1)
	}
}
