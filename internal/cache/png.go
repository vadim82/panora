package cache

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
)

// The PNG decoder ignores animation control chunks. A valid APNG must put its
// animation control chunk before the first image data chunk.
func verifyStillPNG(ctx context.Context, source io.ReaderAt, size int64) error {
	var header [8]byte
	for offset := int64(8); offset <= size-int64(len(header)); {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := source.ReadAt(header[:], offset); err != nil {
			return err
		}
		switch string(header[4:]) {
		case "acTL":
			return fmt.Errorf("animated PNG uploads are unsupported")
		case "IDAT":
			return nil
		}
		offset += int64(binary.BigEndian.Uint32(header[:4])) + 12
	}
	return fmt.Errorf("PNG has no image data")
}
