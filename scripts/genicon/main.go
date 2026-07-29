// 从 resource/im1.png 生成多尺寸 build/windows/icon.ico（Catmull-Rom 重采样）。
// 用法：go run ./scripts/genicon
// 同时输出 build/windows/icon-preview-{16,24,32}.png 供人工核对清晰度。
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"

	xdraw "golang.org/x/image/draw"
)

// 覆盖整数与常见分数 DPI 下 Shell 会取用的全部尺寸
var sizes = []int{16, 20, 24, 32, 40, 48, 64, 128, 256}

func main() {
	srcData, err := os.ReadFile("resource/im1.png")
	if err != nil {
		log.Fatalf("读取源图: %v", err)
	}
	src, _, err := image.Decode(bytes.NewReader(srcData))
	if err != nil {
		log.Fatalf("解码源图: %v", err)
	}

	frames := make(map[int]*image.NRGBA, len(sizes))
	for _, size := range sizes {
		dst := image.NewNRGBA(image.Rect(0, 0, size, size))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
		frames[size] = dst
	}

	if err := writeICO("build/windows/icon.ico", frames); err != nil {
		log.Fatalf("写入 ico: %v", err)
	}
	for _, size := range []int{16, 24, 32} {
		var buf bytes.Buffer
		if err := png.Encode(&buf, frames[size]); err == nil {
			_ = os.WriteFile(fmt.Sprintf("build/windows/icon-preview-%d.png", size), buf.Bytes(), 0o644)
		}
	}
	fmt.Println("icon.ico 已生成，含尺寸:", sizes)
}

// writeICO：≤48px 用经典 BGRA DIB 帧（最大兼容），≥64px 用 PNG 帧（Vista+ 支持，控制体积）
func writeICO(path string, frames map[int]*image.NRGBA) error {
	type entry struct {
		size int
		data []byte
		png  bool
	}
	var entries []entry
	for _, size := range sizes {
		img := frames[size]
		if size >= 64 {
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err != nil {
				return err
			}
			entries = append(entries, entry{size, buf.Bytes(), true})
		} else {
			entries = append(entries, entry{size, encodeDIB(img), false})
		}
	}

	var out bytes.Buffer
	// ICONDIR
	binary.Write(&out, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&out, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(&out, binary.LittleEndian, uint16(len(entries)))

	offset := 6 + 16*len(entries)
	for _, e := range entries {
		dim := byte(e.size)
		if e.size >= 256 {
			dim = 0 // 0 表示 256
		}
		out.WriteByte(dim)                                     // width
		out.WriteByte(dim)                                     // height
		out.WriteByte(0)                                       // palette
		out.WriteByte(0)                                       // reserved
		binary.Write(&out, binary.LittleEndian, uint16(1))     // planes
		binary.Write(&out, binary.LittleEndian, uint16(32))    // bpp
		binary.Write(&out, binary.LittleEndian, uint32(len(e.data)))
		binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(e.data)
	}
	for _, e := range entries {
		out.Write(e.data)
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

// encodeDIB：BITMAPINFOHEADER（高度双倍含 AND 掩码）+ 自底向上 BGRA + 全零掩码
func encodeDIB(img *image.NRGBA) []byte {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	var buf bytes.Buffer

	binary.Write(&buf, binary.LittleEndian, uint32(40))    // header size
	binary.Write(&buf, binary.LittleEndian, int32(w))      // width
	binary.Write(&buf, binary.LittleEndian, int32(h*2))    // height×2（含掩码区）
	binary.Write(&buf, binary.LittleEndian, uint16(1))     // planes
	binary.Write(&buf, binary.LittleEndian, uint16(32))    // bpp
	binary.Write(&buf, binary.LittleEndian, uint32(0))     // BI_RGB
	binary.Write(&buf, binary.LittleEndian, uint32(0))     // size image
	binary.Write(&buf, binary.LittleEndian, [4]uint32{})   // resolutions + colors

	// 像素：自底向上、BGRA
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			i := img.PixOffset(x, y)
			buf.Write([]byte{img.Pix[i+2], img.Pix[i+1], img.Pix[i+0], img.Pix[i+3]})
		}
	}
	// AND 掩码：32bpp 带 alpha 时全零即可，行按 32bit 对齐
	maskStride := ((w + 31) / 32) * 4
	mask := make([]byte, maskStride*h)
	buf.Write(mask)
	return buf.Bytes()
}
