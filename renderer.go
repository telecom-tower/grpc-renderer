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
	"io"
	"net"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/telecom-tower/grpc-renderer/font"
	pb "github.com/telecom-tower/towerapi/v1"
	"google.golang.org/grpc"
)

const (
	displayHeight = 8
	displayWidth  = 256
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
	min := bounds.Min
	max := bounds.Max
	resize := false
	if rect.Min.X < min.X {
		min.X = rect.Min.X
		resize = true
	}
	if rect.Min.Y < min.Y {
		min.Y = rect.Min.Y
		resize = true
	}
	if rect.Max.X > max.X {
		max.X = rect.Max.X
		resize = true
	}
	if rect.Max.Y > max.Y {
		max.Y = rect.Max.Y
		resize = true
	}
	if resize {
		dst := image.NewRGBA(image.Rect(min.X, min.Y, max.X, max.Y))
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
		r0, g0, b0, _ := img.At(x, y).RGBA()
		r1, g1, b1, a1 := c.RGBA()
		r := (r1*a1 + r0*(0xffff-a1)) / 0xffff
		g := (g1*a1 + g0*(0xffff-a1)) / 0xffff
		b := (b1*a1 + b0*(0xffff-a1)) / 0xffff
		img.Set(x, y, color.RGBA64{
			R: uint16(r),
			G: uint16(g),
			B: uint16(b),
			A: 0xFFFF,
		})
	} else {
		img.Set(x, y, c)
	}
}

func (s *TowerRenderer) fill(fill *pb.Fill) error {
	log.Debugf("fill")
	s.activeLayers[fill.Layer] = true
	layer := s.layers[fill.Layer]
	c := pbColorToColor(fill.Color)
	bounds := layer.Bounds()
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			paint(layer, x, y, c, int(fill.PaintMode))
		}
	}
	return nil
}

func (s *TowerRenderer) clear(clear *pb.Clear) error {
	log.Debugf("clear")
	for _, l := range clear.Layer {
		layer := s.layers[l]
		bounds := layer.Bounds()
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				layer.Set(x, y, color.Transparent)
			}
		}
		s.activeLayers[l] = false
	}
	return nil
}

func (s *TowerRenderer) setPixels(pixels *pb.SetPixels) error {
	log.Debugf("set pixels")
	s.activeLayers[pixels.Layer] = true
	layer := s.layers[pixels.Layer]
	for _, pix := range pixels.Pixels {
		point := image.Point{
			X: int(pix.Point.X),
			Y: int(pix.Point.Y),
		}
		layer = resizeImage(
			layer,
			image.Rect(point.X, point.Y, point.X+1, point.Y+1))
		paint(layer, point.X, point.Y, pbColorToColor(pix.Color), int(pixels.PaintMode))
	}
	s.layers[pixels.Layer] = layer
	return nil
}

func (s *TowerRenderer) drawRectangle(rect *pb.DrawRectangle) error {
	log.Debug("draw rectangle")
	s.activeLayers[rect.Layer] = true
	layer := s.layers[rect.Layer]
	c := pbColorToColor(rect.Color)
	r := image.Rect(int(rect.Min.X), int(rect.Min.Y), int(rect.Max.X), int(rect.Max.Y))
	layer = resizeImage(layer, r)
	for x := r.Min.X; x < r.Max.X; x++ {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			paint(layer, x, y, c, int(rect.PaintMode))
		}
	}
	s.layers[rect.Layer] = layer
	return nil
}

func (s *TowerRenderer) drawBitmap(bitmap *pb.DrawBitmap) error {
	log.Debug("draw bitmap")
	bounds := image.Rect(
		int(bitmap.Position.X),
		int(bitmap.Position.Y),
		int(bitmap.Position.X)+int(bitmap.Width),
		int(bitmap.Position.Y)+int(bitmap.Height),
	)
	s.activeLayers[bitmap.Layer] = true
	layer := s.layers[bitmap.Layer]
	resizeImage(layer, bounds)
	i := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			paint(layer, x, y, pbColorToColor(bitmap.Colors[i]), int(bitmap.PaintMode))
			i++
		}
	}
	return nil
}

func (s *TowerRenderer) writeText(wt *pb.WriteText) error {
	log.Debug("write text")
	s.activeLayers[wt.Layer] = true
	layer := s.layers[wt.Layer]
	var fnt font.Font
	var rect image.Rectangle

	msg, err := font.ExpandAlias(wt.Text)
	if err != nil {
		return errors.WithMessage(err, "Error expanding text")
	}

	if wt.Font == "8x8" {
		fnt = font.Font8x8
		rect = image.Rect(int(wt.X), 0, int(wt.X)+8*len(msg), 8)
	} else if wt.Font == "6x8" {
		fnt = font.Font6x8
		rect = image.Rect(int(wt.X), 0, int(wt.X)+6*len(msg), 8)
	} else {
		return errors.New("Unknown font")
	}

	layer = resizeImage(layer, rect)
	c := pbColorToColor(wt.Color)
	x := int(wt.X)
	for _, r := range msg {
		for _, glyph := range fnt.Bitmap[r] {
			for y := 0; y < 8; y++ {
				if uint(glyph)&(1<<uint(y)) != 0 {
					paint(layer, x, y, c, int(wt.PaintMode))
				}
			}
			x++
		}
	}
	s.layers[wt.Layer] = layer
	return nil
}

func (s *TowerRenderer) hScroll(*pb.HScroll) error {
	log.Debug("horizontal scroll (NYI)")
	// TODO: Not yet implemented
	return nil
}

func (s *TowerRenderer) vScroll(*pb.VScroll) error {
	log.Debug("vertical scroll (NYI)")
	// TODO: Not yet implemented
	return nil
}

func (s *TowerRenderer) setLayerAlpha(*pb.SetLayerAlpha) error {
	log.Debug("Set Layer Alpha (NYI)")
	// TODO: Not yet implemented
	return nil
}

func (s *TowerRenderer) roll(*pb.Roll) error {
	log.Debug("Roll (NYI)")
	// TODO: Not yet implemented
	return nil
}

func (s *TowerRenderer) effect(*pb.Effect) error {
	log.Debug("Effect (NYI)")
	// TODO: Not yet implemented
	return nil
}

func (s *TowerRenderer) animate(*pb.Animate) error {
	log.Debug("Animate (NYI)")
	// TODO: Not yet implemented
	return nil
}

func (s *TowerRenderer) renderLeds() {
	result := image.NewRGBA(image.Rect(0, 0, displayWidth, displayHeight))
	for i := 0; i < maxLayers; i++ {
		if s.activeLayers[i] {
			log.Debugf("Layer %v is active", i)
			l := s.layers[i]
			bounds := l.Bounds()
			result = resizeImage(result, bounds)
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
					r0, g0, b0, _ := result.At(x, y).RGBA()
					r1, g1, b1, a1 := l.At(x, y).RGBA()
					r := (r1*a1 + r0*(0xffff-a1)) / 0xffff
					g := (g1*a1 + g0*(0xffff-a1)) / 0xffff
					b := (b1*a1 + b0*(0xffff-a1)) / 0xffff
					result.Set(x, y, color.RGBA64{
						R: uint16(r),
						G: uint16(g),
						B: uint16(b),
						A: 0xFFFF,
					})
				}
			}
		}
	}

	clip := image.Rect(0, 0, displayWidth, displayHeight)
	result = resizeImage(result, clip)
	leds := s.ws.Leds(0)
	for x := 0; x < displayWidth; x++ {
		for y := 0; y < displayHeight; y++ {
			var index int
			if x%2 == 0 {
				index = x*displayHeight + y
			} else {
				index = x*displayHeight + (displayHeight - 1 - y)
			}
			r, g, b, _ := result.At(clip.Min.X+x, clip.Min.Y+y).RGBA()
			c := ((r>>8)&0xff)<<16 + ((g>>8)&0xff)<<8 + ((b>>8)&0xff)<<0
			leds[index] = c
		}
	}
}

// Draw implements the main task of the server, namely drawing on the display
func (s *TowerRenderer) Draw(stream pb.TowerDisplay_DrawServer) error { // nolint: gocyclo
	var status error
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			if status == nil {
				s.renderLeds()
				status = s.ws.Render()
			}
			msg := ""
			if status != nil {
				msg = status.Error()
			}
			return stream.SendAndClose(&pb.DrawResponse{
				Message: msg,
			})
		}
		if err != nil {
			return err
		}

		if status == nil {
			switch t := in.Type.(type) {
			case *pb.DrawRequest_Fill:
				status = s.fill(t.Fill)
			case *pb.DrawRequest_Clear:
				status = s.clear(t.Clear)
			case *pb.DrawRequest_SetPixels:
				status = s.setPixels(t.SetPixels)
			case *pb.DrawRequest_DrawRectangle:
				status = s.drawRectangle(t.DrawRectangle)
			case *pb.DrawRequest_DrawBitmap:
				status = s.drawBitmap(t.DrawBitmap)
			case *pb.DrawRequest_WriteText:
				status = s.writeText(t.WriteText)
			case *pb.DrawRequest_HScroll:
				status = s.hScroll(t.HScroll)
			case *pb.DrawRequest_VScroll:
				status = s.vScroll(t.VScroll)
			case *pb.DrawRequest_SetLayerAlpha:
				status = s.setLayerAlpha(t.SetLayerAlpha)
			case *pb.DrawRequest_Roll:
				status = s.roll(t.Roll)
			case *pb.DrawRequest_Effect:
				status = s.effect(t.Effect)
			case *pb.DrawRequest_Animate:
				status = s.animate(t.Animate)
			}
		}
	}
}

// Serve starts a grpc server and handles the requests
func Serve(listener net.Listener, ws2811 WsEngine, opts ...grpc.ServerOption) error {
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterTowerDisplayServer(grpcServer, NewRenderer(ws2811))
	log.Infof("Telecom Tower Server running at %v\n", listener.Addr().String())
	err := grpcServer.Serve(listener)
	if err != nil {
		return errors.WithMessage(err, "failed to serve")
	}
	return nil
}
