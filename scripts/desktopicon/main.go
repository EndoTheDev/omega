// Command desktopicon owns the omega mark: it renders the icon set, embeds the
// set in the executable as a PE resource, and verifies the built binary.
//
//	desktopicon -ico <path>   render the icon set into <path>
//	desktopicon -syso         embed cmd/omega/assets/omega.ico as a resource
//	desktopicon -check <exe>  report the executable's icon group
package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/tc-hib/winres"
)

const (
	assetPath = "cmd/omega/assets/omega.ico"
	sysoPath  = "cmd/omega/rsrc_windows_amd64.syso"
	groupID   = 1
)

// iconSizes are the Windows shell's asks: 16 taskbar and tree, 24 at 150% DPI,
// 32 the default, 48 Explorer, 64 tiles, 128 and 256 the jumbo views. Windows
// reads PNG entries at 256 only; every other size is a 32bpp DIB.
var iconSizes = []int{16, 24, 32, 48, 64, 128, 256}

// The mark in the unit square, y down. Every length is a fraction of the canvas,
// so one description serves all sizes. The numbers are measured off the 32px
// entry the mark shipped as before this tool existed.
const (
	midpoint  = 0.5 // disc centre
	discEdge  = 0.5 // disc radius: the disc is inscribed, touching all four mid-edges
	ringOuter = 0.320
	ringInner = 0.190
	footTop   = 0.585
	footBase  = 0.710
	footGap   = 0.133 // half the opening between the feet
	grid      = 16    // supersamples per axis
)

var (
	discInk  = color.NRGBA{R: 0x24, G: 0x2A, B: 0x31, A: 0xFF}
	glyphInk = color.NRGBA{R: 0xF5, G: 0xF7, B: 0xFA, A: 0xFF}
)

// footReach is the disc's horizontal half-width at the foot baseline: the feet
// end there, so the omega spans the disc and stays inside it.
var footReach = math.Sqrt(discEdge*discEdge - (footBase-midpoint)*(footBase-midpoint))

// arcEnd is the angle from +x where the ring's stroke meets the foot baseline.
// The arc's opening sits between arcEnd and pi-arcEnd, hidden under the feet.
var arcEnd = math.Asin((footBase - midpoint) / ringOuter)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("desktopicon", flag.ContinueOnError)
	flags.SetOutput(stderr)
	icoOut := flags.String("ico", "", "render the icon set to this path")
	syso := flags.Bool("syso", false, "write "+sysoPath+" from "+assetPath)
	check := flags.String("check", "", "report the icon group of this executable")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	var err error
	switch {
	case *icoOut != "":
		err = writeIconFile(*icoOut)
	case *syso:
		err = writeSyso()
	case *check != "":
		var report string
		report, err = checkExe(*check)
		if err == nil {
			fmt.Fprint(stdout, report)
		}
	default:
		flags.Usage()
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "desktopicon: %v\n", err)
		return 1
	}
	return 0
}

// writeIconFile renders every size and writes one ICO container.
func writeIconFile(path string) error {
	var buf bytes.Buffer
	if err := writeICO(&buf); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func writeICO(w io.Writer) error {
	blobs := make([][]byte, len(iconSizes))
	for i, size := range iconSizes {
		blob, err := encodeEntry(size)
		if err != nil {
			return err
		}
		blobs[i] = blob
	}

	offset := 6 + 16*len(blobs)
	head := make([]byte, offset)
	binary.LittleEndian.PutUint16(head[2:], 1) // type: icon
	binary.LittleEndian.PutUint16(head[4:], uint16(len(blobs)))
	for i, size := range iconSizes {
		entry := head[6+i*16:]
		if size < 256 {
			entry[0] = uint8(size)
			entry[1] = uint8(size)
		}
		binary.LittleEndian.PutUint16(entry[4:], 1)  // planes
		binary.LittleEndian.PutUint16(entry[6:], 32) // bits per pixel
		binary.LittleEndian.PutUint32(entry[8:], uint32(len(blobs[i])))
		binary.LittleEndian.PutUint32(entry[12:], uint32(offset))
		offset += len(blobs[i])
	}
	if _, err := w.Write(head); err != nil {
		return err
	}
	for _, blob := range blobs {
		if _, err := w.Write(blob); err != nil {
			return err
		}
	}
	return nil
}

func encodeEntry(size int) ([]byte, error) {
	img := render(size)
	if size >= 256 {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	return encodeDIB(img, size), nil
}

// encodeDIB writes the 32bpp bottom-up DIB an icon entry carries. The AND mask
// is all zero - alpha decides the shape - but the entry format requires it, one
// row per pixel row, each row padded to four bytes.
func encodeDIB(img *image.NRGBA, size int) []byte {
	const headerSize = 40
	maskStride := ((size + 31) / 32) * 4
	maskSize := maskStride * size
	pixels := size * size * 4

	blob := make([]byte, headerSize+pixels+maskSize)
	header := blob[:headerSize]
	binary.LittleEndian.PutUint32(header[0:], headerSize)
	binary.LittleEndian.PutUint32(header[4:], uint32(size))
	binary.LittleEndian.PutUint32(header[8:], uint32(2*size)) // XOR image + AND mask
	binary.LittleEndian.PutUint16(header[12:], 1)             // planes
	binary.LittleEndian.PutUint16(header[14:], 32)            // bits per pixel
	binary.LittleEndian.PutUint32(header[20:], uint32(pixels+maskSize))

	for y := 0; y < size; y++ {
		row := headerSize + (size-1-y)*size*4
		for x := 0; x < size; x++ {
			c := img.NRGBAAt(x, y)
			pixel := blob[row+x*4:]
			pixel[0], pixel[1], pixel[2], pixel[3] = c.B, c.G, c.R, c.A
		}
	}
	return blob
}

// render draws the mark at one size. Each pixel averages a grid x grid sample
// set, so the disc edge and the glyph edges carry proper coverage alpha and the
// output is byte-identical on every run.
func render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const total = grid * grid
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var glyph, disc int
			for sy := 0; sy < grid; sy++ {
				y := (float64(py) + (float64(sy)+0.5)/grid) / float64(size)
				for sx := 0; sx < grid; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/grid) / float64(size)
					switch {
					case !inDisc(x, y):
					case inRing(x, y) || inFoot(x, y):
						glyph++
					default:
						disc++
					}
				}
			}
			covered := glyph + disc
			if covered == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{
				R: mix(glyph, disc, glyphInk.R, discInk.R),
				G: mix(glyph, disc, glyphInk.G, discInk.G),
				B: mix(glyph, disc, glyphInk.B, discInk.B),
				A: uint8((255*covered + total/2) / total),
			})
		}
	}
	return img
}

// mix is the ink average over the covered samples, in integer arithmetic.
func mix(glyph, disc int, glyphValue, discValue uint8) uint8 {
	covered := glyph + disc
	sum := glyph*int(glyphValue) + disc*int(discValue)
	return uint8((sum + covered/2) / covered)
}

func inDisc(x, y float64) bool {
	dx, dy := x-midpoint, y-midpoint
	return dx*dx+dy*dy <= discEdge*discEdge
}

// inRing is the annulus with the bottom wedge removed: the omega's opening.
func inRing(x, y float64) bool {
	dx, dy := x-midpoint, y-midpoint
	distance := math.Hypot(dx, dy)
	if distance < ringInner || distance > ringOuter {
		return false
	}
	angle := math.Atan2(dy, dx)
	return angle < arcEnd || angle > math.Pi-arcEnd
}

func inFoot(x, y float64) bool {
	if y < footTop || y > footBase {
		return false
	}
	reach := math.Abs(x - midpoint)
	return reach >= footGap && reach <= footReach
}

// writeSyso embeds the asset's icon group in a COFF object the Go linker picks
// up for GOOS=windows GOARCH=amd64. The object is a build artifact: every
// sanctioned build regenerates it and -check proves the result.
func writeSyso() error {
	return writeSysoTo(assetPath, sysoPath)
}

func writeSysoTo(asset, object string) error {
	file, err := os.Open(asset)
	if err != nil {
		return err
	}
	defer file.Close()

	icon, err := winres.LoadICO(file)
	if err != nil {
		return fmt.Errorf("%s: %w", asset, err)
	}
	resources := winres.ResourceSet{}
	if err := resources.SetIcon(winres.ID(groupID), icon); err != nil {
		return err
	}

	out, err := os.Create(object)
	if err != nil {
		return err
	}
	if err := resources.WriteObject(out, winres.ArchAMD64); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

const (
	resourceIcon      = 3
	resourceGroupIcon = 14
)

var errNoResources = errors.New("no resource directory")

// checkExe reads the PE resource tree and reports the icon group and the icon
// entries behind it. An executable with no icon group is an error: the shell
// would fall back to a faceless generic.
func checkExe(path string) (string, error) {
	file, err := pe.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	leaves, err := resourceLeaves(file, raw)
	if errors.Is(err, errNoResources) {
		leaves = nil
	} else if err != nil {
		return "", err
	}

	var report strings.Builder
	groups := 0
	for _, leaf := range leaves {
		if leaf.kind != resourceGroupIcon {
			continue
		}
		sizes, err := groupSizes(leaf.data)
		if err != nil {
			return "", err
		}
		groups++
		sort.Ints(sizes)
		names := make([]string, len(sizes))
		for i, size := range sizes {
			names[i] = fmt.Sprint(size)
		}
		fmt.Fprintf(&report, "icon group %d: %s\n", leaf.name, strings.Join(names, " "))
	}
	if groups == 0 {
		return "", fmt.Errorf("%s carries no icon group", path)
	}
	for _, leaf := range leaves {
		if leaf.kind != resourceIcon {
			continue
		}
		width, height, bits, err := iconEntryInfo(leaf.data)
		if err != nil {
			return "", fmt.Errorf("icon %d: %w", leaf.name, err)
		}
		fmt.Fprintf(&report, "RT_ICON %d: %dx%d %dbpp %d bytes\n", leaf.name, width, height, bits, len(leaf.data))
	}
	return report.String(), nil
}

// iconEntryInfo reports an icon entry's shape: a 32bpp DIB carries its size in
// BITMAPINFOHEADER (height counts the XOR image and the AND mask), a PNG
// carries it in IHDR.
func iconEntryInfo(data []byte) (width, height, bits int, err error) {
	if bytes.HasPrefix(data, []byte("\x89PNG")) {
		config, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return 0, 0, 0, err
		}
		return config.Width, config.Height, 32, nil
	}
	if len(data) < 16 {
		return 0, 0, 0, errors.New("icon entry is truncated")
	}
	width = int(binary.LittleEndian.Uint32(data[4:]))
	height = int(binary.LittleEndian.Uint32(data[8:])) / 2
	bits = int(binary.LittleEndian.Uint16(data[14:]))
	return width, height, bits, nil
}

// groupSizes reads the GRPICONDIR Windows stores for an icon group.
func groupSizes(data []byte) ([]int, error) {
	if len(data) < 6 {
		return nil, errors.New("icon group is truncated")
	}
	count := int(binary.LittleEndian.Uint16(data[4:]))
	if len(data) < 6+14*count {
		return nil, errors.New("icon group directory is truncated")
	}
	sizes := make([]int, count)
	for i := 0; i < count; i++ {
		size := int(data[6+i*14])
		if size == 0 {
			size = 256
		}
		sizes[i] = size
	}
	return sizes, nil
}

type resourceLeaf struct {
	kind uint32
	name uint32
	data []byte
}

// resourceLeaves walks the three-level PE resource tree (type, name, language)
// and returns every leaf's bytes.
func resourceLeaves(file *pe.File, raw []byte) ([]resourceLeaf, error) {
	var directoryRVA, directorySize uint32
	switch header := file.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		directoryRVA = header.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].VirtualAddress
		directorySize = header.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].Size
	case *pe.OptionalHeader32:
		directoryRVA = header.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].VirtualAddress
		directorySize = header.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].Size
	}
	if directoryRVA == 0 || directorySize == 0 {
		return nil, errNoResources
	}
	directory, err := imageBytes(file, raw, directoryRVA, directorySize)
	if err != nil {
		return nil, err
	}

	var leaves []resourceLeaf
	var walk func(offset, depth int, leaf resourceLeaf) error
	walk = func(offset, depth int, leaf resourceLeaf) error {
		if offset < 0 || offset+16 > len(directory) {
			return errors.New("resource directory out of range")
		}
		count := int(binary.LittleEndian.Uint16(directory[offset+12:])) +
			int(binary.LittleEndian.Uint16(directory[offset+14:]))
		for i := 0; i < count; i++ {
			entry := offset + 16 + i*8
			if entry+8 > len(directory) {
				return errors.New("resource directory entry out of range")
			}
			identifier := binary.LittleEndian.Uint32(directory[entry:])
			target := binary.LittleEndian.Uint32(directory[entry+4:])
			if identifier&0x80000000 != 0 { // string-named resource: not ours
				continue
			}
			switch depth {
			case 0:
				leaf.kind = identifier
			case 1:
				leaf.name = identifier
			}
			if target&0x80000000 != 0 {
				if depth < 2 {
					if err := walk(int(target&0x7fffffff), depth+1, leaf); err != nil {
						return err
					}
				}
				continue
			}
			if depth != 2 {
				continue
			}
			dataOffset := int(target)
			if dataOffset+16 > len(directory) {
				return errors.New("resource data entry out of range")
			}
			dataRVA := binary.LittleEndian.Uint32(directory[dataOffset:])
			dataSize := binary.LittleEndian.Uint32(directory[dataOffset+4:])
			data, err := imageBytes(file, raw, dataRVA, dataSize)
			if err != nil {
				return err
			}
			leaf.data = data
			leaves = append(leaves, leaf)
		}
		return nil
	}
	if err := walk(0, 0, resourceLeaf{}); err != nil {
		return nil, err
	}
	return leaves, nil
}

// imageBytes resolves an RVA and length into the file bytes behind it.
func imageBytes(file *pe.File, raw []byte, rva, size uint32) ([]byte, error) {
	for _, section := range file.Sections {
		span := section.VirtualAddress + section.VirtualSize
		if rva < section.VirtualAddress || rva >= span {
			continue
		}
		offset := int(section.Offset) + int(rva-section.VirtualAddress)
		if offset < 0 || offset+int(size) > len(raw) {
			return nil, errors.New("resource data outside the file")
		}
		return raw[offset : offset+int(size)], nil
	}
	return nil, fmt.Errorf("rva %#x is outside every section", rva)
}
