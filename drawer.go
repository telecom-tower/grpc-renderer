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
	"io"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/telecom-tower/grpc-renderer/font"
	pb "github.com/telecom-tower/towerapi/v1"
)

func (tower *TowerRenderer) fill(fill *pb.Fill) error {
	log.Debugf("fill")
	tower.activeLayers[fill.Layer] = true
	layer := tower.layers[fill.Layer]
	layer.dirty = true
	canvas := layer.image
	c := pbColorToColor(fill.Color)
	bounds := canvas.Bounds()
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			paint(canvas, x, y, c, int(fill.PaintMode))
		}
	}
	return nil
}

func (tower *TowerRenderer) clear(clear *pb.Clear) error {
	log.Debugf("clear")
	for _, l := range clear.Layer {
		layer := tower.layers[l]
		layer.image = image.NewRGBA(image.Rect(0, 0, 0, 0))
		layer.dirty = true
		tower.activeLayers[l] = false
	}
	return nil
}

func (tower *TowerRenderer) setPixels(pixels *pb.SetPixels) error {
	log.Debugf("set pixels")
	tower.activeLayers[pixels.Layer] = true
	layer := tower.layers[pixels.Layer]
	canvas := layer.image
	for _, pix := range pixels.Pixels {
		point := image.Point{
			X: int(pix.Point.X),
			Y: int(pix.Point.Y),
		}
		canvas = resizeImage(
			canvas,
			image.Rect(point.X, point.Y, point.X+1, point.Y+1))
		paint(canvas, point.X, point.Y, pbColorToColor(pix.Color), int(pixels.PaintMode))
	}
	tower.layers[pixels.Layer].image = canvas
	return nil
}

func (tower *TowerRenderer) drawRectangle(rect *pb.DrawRectangle) error {
	log.Debug("draw rectangle")
	tower.activeLayers[rect.Layer] = true
	layer := tower.layers[rect.Layer]
	layer.dirty = true
	canvas := layer.image
	c := pbColorToColor(rect.Color)
	r := image.Rect(int(rect.Min.X), int(rect.Min.Y), int(rect.Max.X), int(rect.Max.Y))
	canvas = resizeImage(canvas, r)
	for x := r.Min.X; x < r.Max.X; x++ {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			paint(canvas, x, y, c, int(rect.PaintMode))
		}
	}
	tower.layers[rect.Layer].image = canvas
	return nil
}

func (tower *TowerRenderer) drawBitmap(bitmap *pb.DrawBitmap) error {
	log.Debug("draw bitmap")
	bounds := image.Rect(
		int(bitmap.Position.X),
		int(bitmap.Position.Y),
		int(bitmap.Position.X)+int(bitmap.Width),
		int(bitmap.Position.Y)+int(bitmap.Height),
	)
	tower.activeLayers[bitmap.Layer] = true
	layer := tower.layers[bitmap.Layer]
	layer.dirty = true
	canvas := layer.image
	canvas = resizeImage(canvas, bounds)
	i := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			paint(canvas, x, y, pbColorToColor(bitmap.Colors[i]), int(bitmap.PaintMode))
			i++
		}
	}
	tower.layers[bitmap.Layer].image = canvas
	return nil
}

func (tower *TowerRenderer) writeText(wt *pb.WriteText) error {
	log.Debug("write text")
	tower.activeLayers[wt.Layer] = true
	layer := tower.layers[wt.Layer]
	layer.dirty = true
	canvas := layer.image
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

	canvas = resizeImage(canvas, rect)
	c := pbColorToColor(wt.Color)
	x := int(wt.X)
	for _, r := range msg {
		for _, glyph := range fnt.Bitmap[r] {
			for y := 0; y < 8; y++ {
				if uint(glyph)&(1<<uint(y)) != 0 {
					paint(canvas, x, y, c, int(wt.PaintMode))
				}
			}
			x++
		}
	}
	tower.layers[wt.Layer].image = canvas
	return nil
}

func (tower *TowerRenderer) setLayerOrigin(origin *pb.SetLayerOrigin) error {
	log.Debug("Set Layer Origin")
	tower.activeLayers[origin.Layer] = true
	layer := tower.layers[origin.Layer]
	layer.dirty = true
	layer.origin = image.Point{X: int(origin.Position.X), Y: int(origin.Position.Y)}
	layer.image = resizeImage(
		layer.image,
		image.Rect(
			layer.origin.X,
			layer.origin.Y,
			layer.origin.X+displayWidth,
			layer.origin.Y+displayHeight))
	return nil
}

func (tower *TowerRenderer) setLayerAlpha(alpha *pb.SetLayerAlpha) error {
	log.Debug("Set Layer Alpha")
	tower.activeLayers[alpha.Layer] = true
	layer := tower.layers[alpha.Layer]
	layer.dirty = true
	layer.alpha = int(alpha.Alpha)
	return nil
}

func (tower *TowerRenderer) autoRoll(autoroll *pb.AutoRoll) error {
	log.Debugf("AutoRoll (%v)", autoroll.Mode)
	tower.activeLayers[autoroll.Layer] = true
	layer := tower.layers[autoroll.Layer]
	layer.dirty = true
	layer.rolling.mode = int(autoroll.Mode)
	layer.rolling.entry = int(autoroll.Entry)
	layer.rolling.separator = int(autoroll.Separator)
	return nil
}

// Draw implements the main task of the server, namely drawing on the display
func (tower *TowerRenderer) Draw(stream pb.TowerDisplay_DrawServer) error { // nolint: gocyclo
	var status error
	for i := 0; i < maxLayers; i++ {
		tower.layers[i].dirty = false
	}
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			if status == nil {
				tower.lsc <- tower.getLayersSet()
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
				status = tower.fill(t.Fill)
			case *pb.DrawRequest_Clear:
				status = tower.clear(t.Clear)
			case *pb.DrawRequest_SetPixels:
				status = tower.setPixels(t.SetPixels)
			case *pb.DrawRequest_DrawRectangle:
				status = tower.drawRectangle(t.DrawRectangle)
			case *pb.DrawRequest_DrawBitmap:
				status = tower.drawBitmap(t.DrawBitmap)
			case *pb.DrawRequest_WriteText:
				status = tower.writeText(t.WriteText)
			case *pb.DrawRequest_SetLayerOrigin:
				status = tower.setLayerOrigin(t.SetLayerOrigin)
			case *pb.DrawRequest_SetLayerAlpha:
				status = tower.setLayerAlpha(t.SetLayerAlpha)
			case *pb.DrawRequest_AutoRoll:
				status = tower.autoRoll(t.AutoRoll)
			}
		}
	}
}
