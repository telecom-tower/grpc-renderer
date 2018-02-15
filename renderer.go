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

// TowerRenderer is the base type for rendering
type TowerRenderer struct {
	ws           WsEngine
	layers       []*image.RGBA
	activeLayers []bool
	lsc          chan *layersSet
}

type layersSet struct {
	bounds image.Rectangle
	layers []*image.RGBA
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
	layers := make([]*image.RGBA, maxLayers)
	activeLayers := make([]bool, maxLayers)
	for i := 0; i < len(layers); i++ {
		layers[i] = image.NewRGBA(image.Rect(0, 0, displayWidth, displayHeight))
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

func (tower *TowerRenderer) getLayersSet() *layersSet {
	n := 0
	bounds := image.Rect(0, 0, 0, 0)
	for i := 0; i < maxLayers; i++ {
		if tower.activeLayers[i] {
			layer := tower.layers[i]
			bounds = bounds.Union(layer.Bounds())
			n++
		}
	}
	res := layersSet{
		bounds: bounds,
		layers: make([]*image.RGBA, n),
	}
	j := 0
	for i := 0; i < maxLayers; i++ {
		if tower.activeLayers[i] {
			layer := tower.layers[i]
			bounds := layer.Bounds()
			res.layers[j] = image.NewRGBA(res.bounds)
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
					res.layers[j].Set(x, y, layer.At(x, y))
				}
			}
			j++
		}
	}
	return &res
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
