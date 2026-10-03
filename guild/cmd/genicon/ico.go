package main

import "encoding/binary"

// iconImage 是一个尺寸的图标,PNG 编码。
type iconImage struct {
	size int
	png  []byte
}

// dimByte 是 ICO / GRPICONDIR 目录项里的宽高字段:一个字节,256 记作 0。
func dimByte(size int) byte {
	if size >= 256 {
		return 0
	}
	return byte(size)
}

// encodeICO 打一个多尺寸 .ico:ICONDIR + 每张一条 ICONDIRENTRY + 各张数据。
//
// 每张都直接存 PNG。Vista 起 Windows 认 PNG 压缩的图标项,不限尺寸;
// 老做法是 256 以下存 DIB(带 AND 掩码的位图),只为照顾 XP。
func encodeICO(imgs []iconImage) []byte {
	const dirSize, entrySize = 6, 16
	out := make([]byte, dirSize+entrySize*len(imgs))
	binary.LittleEndian.PutUint16(out[0:], 0) // reserved
	binary.LittleEndian.PutUint16(out[2:], 1) // 1 = 图标(2 是光标)
	binary.LittleEndian.PutUint16(out[4:], uint16(len(imgs)))
	offset := len(out)
	for i, im := range imgs {
		e := out[dirSize+entrySize*i:]
		e[0], e[1] = dimByte(im.size), dimByte(im.size)
		e[2], e[3] = 0, 0                        // 调色板颜色数、保留
		binary.LittleEndian.PutUint16(e[4:], 1)  // planes
		binary.LittleEndian.PutUint16(e[6:], 32) // 位深
		binary.LittleEndian.PutUint32(e[8:], uint32(len(im.png)))
		binary.LittleEndian.PutUint32(e[12:], uint32(offset))
		offset += len(im.png)
	}
	for _, im := range imgs {
		out = append(out, im.png...)
	}
	return out
}
