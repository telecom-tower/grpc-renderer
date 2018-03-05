package renderer

import (
	"image"

	log "github.com/sirupsen/logrus"
	"github.com/telecom-tower/sdk"
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

// This function is rather complex. I should perhaps refactor it
func (tower *TowerRenderer) loop() chan layersSet { // nolint: gocyclo
	log.Debug("Starting tower loop")
	c := make(chan layersSet)

	rollingLayers := make([]rollingLayer, maxLayers)
	for i := 0; i < maxLayers; i++ {
		rollingLayers[i].queue = make(layersSet, 0)
	}
	hasRollingLayers := false

	go func() {
		var currentSet layersSet
		for {
			var newSet bool
			if hasRollingLayers {
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
				hasRollingLayers = false
				log.Debug("Received new set")
				for _, l := range currentSet {
					switch l.rolling.mode {
					case sdk.RollingStop:
						rollingLayers[l.id].reset()
					case sdk.RollingStart:
						rollingLayers[l.id].reset()
						rollingLayers[l.id].enqueue(l)
						rollingLayers[l.id].setPos(0)
						hasRollingLayers = true
					case sdk.RollingContinue:
						hasRollingLayers = true
					case sdk.RollingNext:
						rollingLayers[l.id].enqueue(l)
						l.rolling.mode = sdk.RollingContinue
						hasRollingLayers = true
					}
				}
			}

			if hasRollingLayers {
				for _, l := range currentSet {
					switch l.rolling.mode {
					case sdk.RollingStart:
						l.rolling.mode = sdk.RollingContinue
					case sdk.RollingContinue:
						rollingLayers[l.id].advance()
					}
				}
			}

			toDisplay := make(layersSet, 0)
			for _, l := range currentSet {
				if l.rolling.mode == sdk.RollingContinue {
					toDisplay = append(toDisplay, rollingLayers[l.id].queue[0])
				} else {
					toDisplay = append(toDisplay, l)
				}
			}

			_ = tower.renderLed(toDisplay)
		}
	}()
	return c
}
