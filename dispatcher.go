package renderer

import (
	"image"

	"github.com/telecom-tower/sdk"

	log "github.com/sirupsen/logrus"
)

func (tower *TowerRenderer) renderLed(ls layersSet) error {
	// t0 := time.Now()
	result := image.NewRGBA(image.Rect(0, 0, displayWidth, displayHeight))
	for _, layer := range ls {
		// log.Debugf("Render LEDS using origin %v", layer.origin)
		x0, y0 := layer.origin.X, layer.origin.Y
		for x := 0; x < displayWidth; x++ {
			for y := 0; y < displayHeight; y++ {
				result.Set(x, y, combineOver(result.At(x, y), layer.image.At(x0+x, y0+y)))
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
	// log.Debugf("Rendering time: %f µs", time.Since(t0).Seconds()*1e6)
	return tower.ws.Render()
}

func (tower *TowerRenderer) loop() chan layersSet {
	log.Debug("Starting tower loop")
	c := make(chan layersSet)

	rollingLayers := make(layersSet, 0, maxLayers)
	rollingPos := make([]int, maxLayers)

	go func() {
		var currentSet layersSet
		for {
			var newSet bool
			if len(rollingLayers) > 0 {
				select {
				case currentSet = <-c:
					newSet = true
				default:
					newSet = false
				}
			} else {
				currentSet = <-c
				newSet = true
			}

			if newSet {
				log.Debug("Received new set")
				rollingLayers = rollingLayers[:0]
				for _, l := range currentSet {
					if l.rolling.mode != sdk.RollingStop {
						log.Debug("New rolling layer")
						if l.rolling.mode == sdk.RollingStart {
							rollingPos[l.id] = 0
						}
						rollingLayers = append(rollingLayers, l)
					}
				}
			}

			for _, l := range rollingLayers {
				pos := rollingPos[l.id]
				if pos+displayWidth < l.image.Bounds().Max.X {
					pos++
				} else {
					pos = displayWidth - 1 + l.rolling.entry
				}
				l.origin.X = pos
				rollingPos[l.id] = pos
			}

			_ = tower.renderLed(currentSet)
		}
	}()
	return c
}
