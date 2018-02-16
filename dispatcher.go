package renderer

import (
	"image"
	"time"

	log "github.com/sirupsen/logrus"
)

func (tower *TowerRenderer) renderLed(ls *layersSet) error {
	t0 := time.Now()
	result := image.NewRGBA(image.Rect(0, 0, displayWidth, displayHeight))
	for _, layer := range ls.layers {
		for x := 0; x < displayWidth; x++ {
			for y := 0; y < displayHeight; y++ {
				result.Set(x, y, combineOver(result.At(x, y), layer.At(x, y)))
			}
		}
	}
	leds := tower.ws.Leds(0)
	for x := 0; x < displayWidth; x++ {
		for y := 0; y < displayHeight; y++ {
			var index int
			if x%2 == 0 {
				index = x*displayHeight + y
			} else {
				index = x*displayHeight + (displayHeight - 1 - y)
			}
			r, g, b, _ := result.At(x, y).RGBA()
			c := ((r>>8)&0xff)<<16 + ((g>>8)&0xff)<<8 + ((b>>8)&0xff)<<0
			leds[index] = c
		}
	}
	log.Debugf("Rendering time: %f µs", time.Now().Sub(t0).Seconds()*1e6)
	return tower.ws.Render()
}

func (tower *TowerRenderer) loop() chan *layersSet {
	log.Debug("Starting tower loop")
	c := make(chan *layersSet)
	roll := make(chan struct{})
	go func() {
		var currentSet *layersSet
		for {
			select {
			case currentSet = <-c:
				_ = tower.renderLed(currentSet)
			case <-roll:
				_ = tower.renderLed(currentSet)
			}
		}
	}()
	return c
}
