// Copyright 2018 Jacques Supcik / HEIA-FR
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This package implements the link between a gRPC server and a ws281x "neopixel"
// device. It is a part of the telecom tower project

//go:generate protoc -I $GOPATH/src/github.com/telecom-tower/towerapi/v1 telecomtower.proto --go_out=plugins=grpc:$GOPATH/src/github.com/telecom-tower/towerapi/v1

package renderer

import (
	"image"
	"image/color"
	"net"

	"github.com/telecom-tower/sdk"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	pb "github.com/telecom-tower/towerapi/v1"
	"google.golang.org/grpc"
)

const (
	displayHeight = 8
	displayWidth  = 128
	maxLayers     = 8
)

// WsEngine is an interface to a ws281x "NeoPixel" device
type WsEngine interface {
	Init() error
	Render() error
	Wait() error
	Fini()
	Leds(channel int) []uint32
}

type rolling struct {
	mode      int
	entry     int
	separator int
	last      int
}

type layer struct {
	image   *image.RGBA
	origin  image.Point
	alpha   int
	id      int // the id of a layer is also its zIndex
	dirty   bool
	rolling rolling
}

type layersSet []*layer

// TowerRenderer is the base type for rendering
type TowerRenderer struct {
	ws           WsEngine
	layers       layersSet
	activeLayers []bool
	lsc          chan layersSet
}

func combineOver(bg color.Color, fg color.Color) color.Color {
	r0, g0, b0, _ := bg.RGBA()
	r1, g1, b1, a1 := fg.RGBA()
	r := (r1*a1 + r0*(0xffff-a1)) / 0xffff
	g := (g1*a1 + g0*(0xffff-a1)) / 0xffff
	b := (b1*a1 + b0*(0xffff-a1)) / 0xffff
	return color.RGBA64{
		R: uint16(r),
		G: uint16(g),
		B: uint16(b),
		A: 0xFFFF,
	}
}

// NewRenderer returns a new TowerRenderer instance
func NewRenderer(ws WsEngine) *TowerRenderer {
	layers := make([]*layer, maxLayers)
	activeLayers := make([]bool, maxLayers)
	for i := 0; i < len(layers); i++ {
		layers[i] = &layer{
			image:  image.NewRGBA(image.Rect(0, 0, 0, 0)),
			origin: image.Point{0, 0},
			alpha:  0xffff,
		}
		activeLayers[i] = false
	}
	return &TowerRenderer{
		ws:           ws,
		layers:       layers,
		activeLayers: activeLayers,
	}
}

func pbColorToColor(c *pb.Color) color.Color {
	return color.RGBA{
		R: uint8(c.Red),
		G: uint8(c.Green),
		B: uint8(c.Blue),
		A: uint8(c.Alpha),
	}
}

func resizeImage(src *image.RGBA, rect image.Rectangle) *image.RGBA {
	bounds := src.Bounds()
	newBounds := bounds.Union(rect)
	if !newBounds.Eq(bounds) {
		dst := image.NewRGBA(newBounds)
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				dst.Set(x, y, src.At(x, y))
			}
		}
		return dst
	}
	return src
}

func paint(img *image.RGBA, x int, y int, c color.Color, mode int) {
	if mode == int(pb.PaintMode_OVER) {
		img.Set(x, y, combineOver(img.At(x, y), c))
	} else {
		img.Set(x, y, c)
	}
}

func preparedLayer(l *layer) *layer {
	log.Debug("Preparing layer")
	res := &layer{
		alpha:  0xffff,
		origin: l.origin,
		rolling: rolling{
			mode:      l.rolling.mode,
			entry:     l.rolling.entry,
			separator: l.rolling.separator,
		},
	}

	// create a new image applying the alpha channel of the layer
	bounds := l.image.Bounds()
	img := image.NewRGBA(bounds)
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			r, g, b, a := l.image.At(x, y).RGBA()
			img.Set(
				x, y,
				color.RGBA64{
					uint16(r), uint16(g), uint16(b),
					uint16(a * uint32(l.alpha) / 0xFFFF),
				})
		}
	}

	if l.rolling.mode == sdk.RollingStop {
		res.image = img
	} else {
		log.Debug("Extending image for rolling")
		wEntry := l.rolling.entry
		wSep := l.rolling.separator
		wBody := l.image.Bounds().Max.X - wEntry - wSep
		// find n such that : wBody + n * (wBody + wSep) >= displayWidth
		// n >= (displayWidth - wBody) / (wBody + wSep)
		// n = (displayWidth - wBody + wBody + wSep - 1) div (wBody + wSep)
		// n = (displayWidth + wSep - 1) div (wBody + wSep)
		nBody := (displayWidth + wSep - 1) / (wBody + wSep)
		wTot := 2*(displayWidth-1) + wEntry + (nBody+1)*(wBody+wSep)
		extendedImg := image.NewRGBA(image.Rect(0, 0, wTot, displayHeight))
		for y := 0; y < displayHeight; y++ {
			// Leave room for an emtpy prolog and copy entry
			for x := 0; x < wEntry; x++ {
				extendedImg.Set(x+displayWidth-1, y, img.At(x, y))
			}
			// Copy extended body and separator
			for nb := 0; nb < nBody+1; nb++ {
				for x := 0; x < wBody+wSep; x++ {
					extendedImg.Set(
						x+displayWidth-1+wEntry+nb*(wBody+wSep),
						y,
						img.At(x+wEntry, y))
				}
			}
			// Copy the start of the body at the end for a seamless rolling
			for x := 0; x < displayWidth-1; x++ {
				extendedImg.Set(
					x+displayWidth-1+wEntry+(nBody+1)*(wBody+wSep),
					y,
					extendedImg.At(x+displayWidth-1+wEntry, y))
			}
		}
		res.origin = image.Point{0, 0}
		res.image = extendedImg
	}
	return res
}

func (tower *TowerRenderer) getLayersSet() layersSet {
	log.Debug("making layer set")
	res := make([]*layer, 0, maxLayers)
	for i := 0; i < maxLayers; i++ {
		if tower.activeLayers[i] {
			log.Debug("Building layer")
			l := tower.layers[i]
			l.id = i
			res = append(res, preparedLayer(l))
		}
	}
	return res
}

// Serve starts a grpc server and handles the requests
func Serve(listener net.Listener, ws2811 WsEngine, opts ...grpc.ServerOption) error {
	grpcServer := grpc.NewServer(opts...)
	tower := NewRenderer(ws2811)
	tower.lsc = tower.loop()
	pb.RegisterTowerDisplayServer(grpcServer, tower)
	log.Infof("Telecom Tower Server running at %v\n", listener.Addr().String())
	err := grpcServer.Serve(listener)
	if err != nil {
		return errors.WithMessage(err, "failed to serve")
	}
	return nil
}
