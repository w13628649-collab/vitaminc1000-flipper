package main

import (
	"encoding/binary"
	"slices"
	"unicode/utf16"
)

// Windows 资源类型和本程序用到的资源 ID。
const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16

	// iconGroupID 是图标组的整数 ID。cmd/client 里的 iconResourceID 必须和它一样:
	// go-webview2 用 LoadImageW(hInstance, IconId, IMAGE_ICON, …) 按整数 ID 取窗口图标;
	// 资源管理器取 exe 图标、快捷方式的 "exe,0" 取的都是第一个图标组,只有一个组就是它。
	// 两边对不上的话 cmd/client 的 TestIconResourceLoads 会红
	iconGroupID = 1
	versionID   = 1

	// langNeutral:资源都标成语言中性。加载器按语言找资源时中性总在候选里,
	// 中英文系统都不会落空
	langNeutral = 0
)

// resource 是一条 Windows 资源。
type resource struct {
	typ, id uint16
	data    []byte
}

// iconResources 是图标需要的两类资源:每个尺寸一条 RT_ICON(ID 从 1 起),
// 外加一条 RT_GROUP_ICON 把它们串起来。RT_ICON 和 RT_GROUP_ICON 的 ID 各管各的。
func iconResources(imgs []iconImage) []resource {
	res := make([]resource, 0, len(imgs)+1)
	// GRPICONDIR:和 .ico 的 ICONDIR 几乎一样,只是每项 14 字节,
	// 最后一个字段是 RT_ICON 的资源 ID 而不是文件偏移
	grp := make([]byte, 6+14*len(imgs))
	binary.LittleEndian.PutUint16(grp[2:], 1)
	binary.LittleEndian.PutUint16(grp[4:], uint16(len(imgs)))
	for i, im := range imgs {
		id := uint16(i + 1)
		res = append(res, resource{rtIcon, id, im.png})
		e := grp[6+14*i:]
		e[0], e[1] = dimByte(im.size), dimByte(im.size)
		binary.LittleEndian.PutUint16(e[4:], 1)
		binary.LittleEndian.PutUint16(e[6:], 32)
		binary.LittleEndian.PutUint32(e[8:], uint32(len(im.png)))
		binary.LittleEndian.PutUint16(e[12:], id)
	}
	return append(res, resource{rtGroupIcon, iconGroupID, grp})
}

// versionStrings 是"属性 → 详细信息"里那几行。FileDescription 也是任务管理器
// 进程列表里显示的名字,和窗口标题(cmd/client 的 appName)保持一致。
//
// 真实版本号是构建时 -X main.version 注入的(客户端日志和诊断上报里有);
// 这个 .syso 是提交在仓库里的静态文件,写不进去,所以数字版本(VS_FIXEDFILEINFO)留 0。
// FileVersion 这一行却不能省:.NET 的 FileVersionInfo(PowerShell 的
// (Get-Item x.exe).VersionInfo、Get-Process 的 Description 都走它)
// 看到 FileVersion 为空就认定这张字符串表不对,去试几个英文代码页,
// 最后把公司名、描述全清空。
var versionStrings = [][2]string{
	{"CompanyName", "albion guild"},
	{"FileDescription", "Albion 行情终端"},
	{"FileVersion", "0.0.0.0"},
	{"InternalName", "flipper-client"},
	{"OriginalFilename", "flipper-client.exe"},
	{"ProductName", "albion guild"},
}

// versionInfo 编 VS_VERSIONINFO。每个节点都是同一种结构:
// wLength、wValueLength、wType、以 NUL 结尾的 UTF-16 键名、补齐到 4 字节、值、
// 补齐、子节点。wLength 包含全部子节点。
func versionInfo() []byte {
	// VS_FIXEDFILEINFO,没列出来的字段(版本号、日期、子类型)都是 0
	fixed := make([]byte, 52)
	binary.LittleEndian.PutUint32(fixed[0:], 0xFEEF04BD)  // dwSignature
	binary.LittleEndian.PutUint32(fixed[4:], 0x00010000)  // dwStrucVersion
	binary.LittleEndian.PutUint32(fixed[24:], 0x3F)       // dwFileFlagsMask
	binary.LittleEndian.PutUint32(fixed[32:], 0x00040004) // dwFileOS = VOS_NT_WINDOWS32
	binary.LittleEndian.PutUint32(fixed[36:], 1)          // dwFileType = VFT_APP

	var strs [][]byte
	for _, kv := range versionStrings {
		v := utf16z(kv[1])
		// 字符串节点的 wValueLength 按 WORD 计,含结尾 NUL
		strs = append(strs, vsNode(kv[0], v, uint16(len(v)/2), 1))
	}
	// StringTable 的键是 8 位十六进制:语言 0000(中性)+ 代码页 04b0(UTF-16)。
	// VarFileInfo\Translation 必须报同一对,查询方靠它拼出 StringTable 的键
	table := vsNode("000004b0", nil, 0, 1, strs...)
	translation := vsNode("Translation", []byte{0x00, 0x00, 0xb0, 0x04}, 4, 0)
	return vsNode("VS_VERSION_INFO", fixed, uint16(len(fixed)), 0,
		vsNode("StringFileInfo", nil, 0, 1, table),
		vsNode("VarFileInfo", nil, 0, 1, translation))
}

func vsNode(key string, value []byte, valueLen, typ uint16, children ...[]byte) []byte {
	b := make([]byte, 6, 64)
	binary.LittleEndian.PutUint16(b[2:], valueLen)
	binary.LittleEndian.PutUint16(b[4:], typ)
	b = append(b, utf16z(key)...)
	b = pad4(b)
	b = append(b, value...)
	for _, c := range children {
		b = pad4(b)
		b = append(b, c...)
	}
	binary.LittleEndian.PutUint16(b[0:], uint16(len(b)))
	return b
}

func utf16z(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 0, 2*len(u)+2)
	for _, c := range u {
		b = binary.LittleEndian.AppendUint16(b, c)
	}
	return append(b, 0, 0)
}

func pad4(b []byte) []byte {
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// encodeRsrc 把资源排成 .rsrc 节的内容:三层目录(类型 → ID → 语言)、
// 数据项、数据。
//
// 数据项里的 OffsetToData 在链接后的 exe 里是 RVA。这里先填节内偏移,
// 返回的 relocs 是这些字段在节内的位置,由链接器加上节的 RVA。
func encodeRsrc(res []resource) (raw []byte, relocs []uint32) {
	res = slices.Clone(res)
	// 同一层目录里 ID 必须升序,加载器是二分查找
	slices.SortFunc(res, func(a, b resource) int {
		if a.typ != b.typ {
			return int(a.typ) - int(b.typ)
		}
		return int(a.id) - int(b.id)
	})
	var groups [][]resource // 按类型分组,顺序跟着 res
	for _, r := range res {
		if n := len(groups); n > 0 && groups[n-1][0].typ == r.typ {
			groups[n-1] = append(groups[n-1], r)
		} else {
			groups = append(groups, []resource{r})
		}
	}
	dirSize := func(entries int) int { return 16 + 8*entries }

	// 先排版:根目录、各类型目录、各 ID 目录,然后数据项,最后数据(8 字节对齐)
	off := dirSize(len(groups))
	typeDir := make([]int, len(groups))
	for g, rs := range groups {
		typeDir[g] = off
		off += dirSize(len(rs))
	}
	idDir, entry, data := make([]int, len(res)), make([]int, len(res)), make([]int, len(res))
	for i := range res {
		idDir[i] = off
		off += dirSize(1)
	}
	for i := range res {
		entry[i] = off
		off += 16
	}
	for i, r := range res {
		off = (off + 7) &^ 7
		data[i] = off
		off += len(r.data)
	}
	raw = make([]byte, (off+7)&^7)

	le32 := binary.LittleEndian.PutUint32
	const subdir = 0x80000000 // 目录项指向下一层目录时置最高位
	// IMAGE_RESOURCE_DIRECTORY:Characteristics、时间戳、版本号都留 0,
	// 只有按 ID 的项、没有按名字的项;后面紧跟每项 8 字节(ID,偏移)
	writeDir := func(at int, entries [][2]uint32) {
		binary.LittleEndian.PutUint16(raw[at+14:], uint16(len(entries)))
		for k, e := range entries {
			le32(raw[at+16+8*k:], e[0])
			le32(raw[at+16+8*k+4:], e[1])
		}
	}

	var root [][2]uint32
	i := 0
	for g, rs := range groups {
		root = append(root, [2]uint32{uint32(rs[0].typ), subdir | uint32(typeDir[g])})
		var ids [][2]uint32
		for _, r := range rs {
			ids = append(ids, [2]uint32{uint32(r.id), subdir | uint32(idDir[i])})
			// 语言这一层是叶子:直接指向数据项,不带最高位
			writeDir(idDir[i], [][2]uint32{{langNeutral, uint32(entry[i])}})
			// IMAGE_RESOURCE_DATA_ENTRY:OffsetToData、Size、CodePage、Reserved
			le32(raw[entry[i]:], uint32(data[i]))
			le32(raw[entry[i]+4:], uint32(len(r.data)))
			relocs = append(relocs, uint32(entry[i]))
			copy(raw[data[i]:], r.data)
			i++
		}
		writeDir(typeDir[g], ids)
	}
	writeDir(0, root)
	return raw, relocs
}

// encodeSyso 把资源包成一个 COFF 目标文件(amd64),go build 会把包目录里的
// *_windows_amd64.syso 直接交给链接器。
//
// 结构和 rc.exe + cvtres 产出的 .res.obj 一样:一个 .rsrc 节、一个指向节本身的
// 静态符号、每个数据项一条 IMAGE_REL_AMD64_ADDR32NB 重定位。Go 的内部链接器
// (cmd/link/internal/loadpe)认这种格式,把 .rsrc 合进 exe 的资源目录。
func encodeSyso(res []resource) []byte {
	raw, relocs := encodeRsrc(res)
	const (
		fileHeader    = 20
		sectionHeader = 40
		relocSize     = 10
		symbolSize    = 18

		machineAMD64     = 0x8664
		relAMD64Addr32NB = 3
		// IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ
		scnFlags = 0x00000040 | 0x40000000
		// IMAGE_SYM_CLASS_STATIC
		classStatic = 3
	)
	rawAt := fileHeader + sectionHeader
	relAt := rawAt + len(raw)
	symAt := relAt + relocSize*len(relocs)

	out := make([]byte, rawAt, symAt+symbolSize+4)
	le16, le32 := binary.LittleEndian.PutUint16, binary.LittleEndian.PutUint32
	le16(out[0:], machineAMD64)
	le16(out[2:], 1) // 节数
	le32(out[8:], uint32(symAt))
	le32(out[12:], 1) // 符号数

	s := out[fileHeader:]
	copy(s[0:8], ".rsrc")
	le32(s[16:], uint32(len(raw))) // SizeOfRawData
	le32(s[20:], uint32(rawAt))    // PointerToRawData
	le32(s[24:], uint32(relAt))    // PointerToRelocations
	le16(s[32:], uint16(len(relocs)))
	le32(s[36:], scnFlags)

	out = append(out, raw...)
	for _, r := range relocs {
		var rel [relocSize]byte
		le32(rel[0:], r) // 要改的字段在节内的偏移
		le32(rel[4:], 0) // 符号 0,也就是 .rsrc 节本身
		le16(rel[8:], relAMD64Addr32NB)
		out = append(out, rel[:]...)
	}
	var sym [symbolSize]byte
	copy(sym[0:8], ".rsrc")
	le16(sym[12:], 1) // 所在节,从 1 数
	sym[16] = classStatic
	out = append(out, sym[:]...)
	return binary.LittleEndian.AppendUint32(out, 4) // 空字符串表:只有它自己的长度
}
