package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"log"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
)

const (
	quitKey  = 24
	clearKey = 54
)

func main() {
	strokeWidth := flag.Int("stroke-width", 2, "the stroke width of the pencil")
	color := flag.Uint("color", 0xDC143C, "the color of the pencil (hex)")
	flag.Parse()

	x, err := xgb.NewConn()
	if err != nil {
		log.Fatalf("could not connect to X server: %v", err)
	}
	defer x.Close()

	setup := xproto.Setup(x)
	screen := setup.DefaultScreen(x)

	img, err := captureScreen(x, screen.Root, int(screen.HeightInPixels), int(screen.WidthInPixels))
	if err != nil {
		log.Printf("could not capture screen: %v", err)
		return
	}

	width, height := img.Bounds().Dx(), img.Bounds().Dy()

	win, err := createWindow(x, screen, width, height)
	if err != nil {
		log.Printf("could not create window: %v", err)
		return
	}

	if err := setFullscreen(x, win); err != nil {
		log.Printf("could not set fullscreen: %v", err)
		return
	}

	pixmapID, err := xproto.NewPixmapId(x)
	if err != nil {
		log.Printf("could not create pixmap: %v", err)
		return
	}

	if err := xproto.CreatePixmapChecked(
		x,
		screen.RootDepth,
		pixmapID,
		xproto.Drawable(win),
		uint16(width),
		uint16(height),
	).Check(); err != nil {
		log.Printf("could not create pixmap: %v", err)
		return
	}
	defer xproto.FreePixmap(x, pixmapID)

	gcID, err := xproto.NewGcontextId(x)
	if err != nil {
		log.Printf("could not create graphics context id: %v\n", err)
		return
	}

	if err := xproto.CreateGCChecked(x, gcID, xproto.Drawable(pixmapID), 0, []uint32{}).Check(); err != nil {
		log.Printf("could not create gccontext: %v", err)
		return
	}
	defer xproto.FreeGC(x, gcID)

	if err := drawImage(x, xproto.Drawable(pixmapID), gcID, img); err != nil {
		log.Printf("could not draw screenshot: %v", err)
		return
	}

	xproto.MapWindow(x, win)

	if err := xproto.ChangeGCChecked(
		x,
		gcID,
		xproto.GcForeground,
		[]uint32{uint32(*color)},
	).Check(); err != nil {
		log.Printf("could not change gccontext: %v", err)
		return
	}

	cursorFontID, err := xproto.NewFontId(x)
	if err != nil {
		log.Printf("could not create cursor font id: %v", err)
		return
	}

	if err := xproto.OpenFontChecked(x, cursorFontID, uint16(len("cursor")), "cursor").Check(); err != nil {
		log.Printf("could not open font: %v", err)
		return
	}
	cursorID, err := xproto.NewCursorId(x)
	if err != nil {
		log.Printf("could not create cursor id: %v", err)
		return
	}

	if err := xproto.CreateGlyphCursorChecked(x, cursorID, cursorFontID, cursorFontID,
		86, 87, 0, 0, 0, 0xffff, 0xffff, 0xffff).Check(); err != nil {
		log.Printf("could not create cursor glyph: %v", err)
		return
	}
	defer xproto.CloseFont(x, cursorFontID)

	if err := xproto.ChangeWindowAttributesChecked(x, win, xproto.CwCursor, []uint32{uint32(cursorID)}).Check(); err != nil {
		log.Printf("could not change windows cursor: %v", err)
		return
	}

	var drag bool
	for {
		e, err := x.WaitForEvent()
		if err != nil {
			log.Printf("error caught in event loop: %v", err)
			return
		}

		switch ev := e.(type) {
		case xproto.ExposeEvent:
			if err := copyPixmap(x,
				pixmapID,
				gcID,
				win,
				uint16(width),
				uint16(height),
			); err != nil {
				log.Printf("could not copy pixmap to window: %v", err)
			}
		case xproto.ButtonPressEvent:
			if ev.Detail == 1 {
				drag = true
			}
		case xproto.KeyPressEvent:
			switch ev.Detail {
			case quitKey:
				log.Println("quitting")
				return
			case clearKey:
				if err := copyPixmap(x,
					pixmapID,
					gcID,
					win,
					uint16(width),
					uint16(height),
				); err != nil {
					log.Printf("could not copy pixmap to window: %v", err)
				}
			}
		case xproto.ButtonReleaseEvent:
			if ev.Detail == 1 {
				drag = false
			}
		case xproto.MotionNotifyEvent:
			if drag {
				xproto.PolyRectangle(x,
					xproto.Drawable(win),
					gcID,
					[]xproto.Rectangle{
						{
							X:      ev.EventX,
							Y:      ev.EventY,
							Width:  uint16(*strokeWidth),
							Height: uint16(*strokeWidth),
						},
					})
			}
		}
	}
}

func intern(x *xgb.Conn, name string) (xproto.Atom, error) {
	r, err := xproto.InternAtom(x, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, fmt.Errorf("interning %s: %w", name, err)
	}

	return r.Atom, nil
}

func setFullscreen(x *xgb.Conn, win xproto.Window) error {
	fullscreen, err := intern(x, "_NET_WM_STATE_FULLSCREEN")
	if err != nil {
		return fmt.Errorf("interning fullscreen: %w", err)
	}

	prop, err := intern(x, "_NET_WM_STATE")
	if err != nil {
		return fmt.Errorf("interning prop: %w", err)
	}

	typ, err := intern(x, "ATOM")
	if err != nil {
		return fmt.Errorf("interning atom type: %w", err)
	}

	buf := make([]byte, 4)
	xgb.Put32(buf, uint32(fullscreen))

	if err := xproto.ChangePropertyChecked(x,
		xproto.PropModeReplace,
		win,
		prop,
		typ,
		32,
		1,
		buf,
	).Check(); err != nil {
		return fmt.Errorf("setting fullscreen atom: %w", err)
	}

	return nil
}

func captureScreen(x *xgb.Conn, root xproto.Window, height, width int) (image.Image, error) {
	reply, err := xproto.GetImage(x,
		xproto.ImageFormatZPixmap,
		xproto.Drawable(root),
		0,
		0,
		uint16(width),
		uint16(height),
		0xffffffff,
	).Reply()
	if err != nil {
		return nil, fmt.Errorf("getting image: %w", err)
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := (y*width + x) * 4
			b := reply.Data[idx]
			g := reply.Data[idx+1]
			r := reply.Data[idx+2]
			a := uint8(255)

			img.Set(x, y, color.RGBA{r, g, b, a})
		}
	}

	return img, nil
}

func createWindow(
	conn *xgb.Conn,
	screen *xproto.ScreenInfo,
	width, height int,
) (xproto.Window, error) {
	winID, err := xproto.NewWindowId(conn)
	if err != nil {
		return 0, fmt.Errorf("creating window: %w", err)
	}

	if err := xproto.CreateWindowChecked(
		conn,
		screen.RootDepth,
		winID,
		screen.Root,
		0,
		0,
		uint16(width),
		uint16(height),
		0,
		xproto.WindowClassInputOutput,
		screen.RootVisual,
		xproto.CwBackPixel|xproto.CwEventMask,
		[]uint32{
			screen.WhitePixel,
			xproto.EventMaskExposure |
				xproto.EventMaskButtonPress |
				xproto.EventMaskButtonRelease |
				xproto.EventMaskPointerMotion |
				xproto.EventMaskKeyPress,
		},
	).Check(); err != nil {
		return 0, fmt.Errorf("creating window: %w", err)
	}

	return winID, nil
}

func copyPixmap(
	conn *xgb.Conn,
	pixmapID xproto.Pixmap,
	gcID xproto.Gcontext,
	win xproto.Window,
	width, height uint16,
) error {
	return xproto.CopyAreaChecked(
		conn,
		xproto.Drawable(pixmapID),
		xproto.Drawable(win),
		gcID,
		0,
		0,
		0,
		0,
		uint16(width),
		uint16(height),
	).Check()
}

func drawImage(
	conn *xgb.Conn,
	drawable xproto.Drawable,
	gc xproto.Gcontext,
	img image.Image,
) error {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()

	data := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			idx := (y*width + x) * 4
			data[idx] = byte(b)
			data[idx+1] = byte(g)
			data[idx+2] = byte(r)
			data[idx+3] = byte(a)
		}
	}

	rowStride := width * 4
	for y := 0; y < height; y++ {
		if err := xproto.PutImageChecked(
			conn,
			xproto.ImageFormatZPixmap,
			drawable,
			gc,
			uint16(width),
			1,
			0,
			int16(y),
			0,
			24,
			data[y*rowStride:(y+1)*rowStride],
		).Check(); err != nil {
			return fmt.Errorf("putting line: %w", err)
		}
	}

	return nil
}
