package store

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// Vector files are a small binary format rather than JSON.
//
// The digested corpus is meant to be greppable, and documents and chunks are
// JSONL for exactly that reason. Vectors are the exception because there is
// nothing to read: 768 floats per chunk is not information a human extracts by
// eye, and storing them as JSON text would roughly triple the size of the corpus
// and introduce a float-formatting round trip that determinism does not need.
const (
	vecMagic   = "CORVEC1\x00"
	vecHeader  = len(vecMagic) + 8 // magic, then dims and count as uint32
	maxVecDims = 1 << 16
)

// WriteVectors writes vectors in order, all of the same width.
//
// Order is the contract: vector i belongs to chunk i of the chunks file written
// alongside it. Nothing in the vector file identifies a chunk, which keeps it
// compact and means the two files must be written and replaced together.
func WriteVectors(path string, vecs [][]float32) error {
	dims := 0
	if len(vecs) > 0 {
		dims = len(vecs[0])
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer f.Close()

	buf := make([]byte, vecHeader)
	copy(buf, vecMagic)
	binary.LittleEndian.PutUint32(buf[len(vecMagic):], uint32(dims))
	binary.LittleEndian.PutUint32(buf[len(vecMagic)+4:], uint32(len(vecs)))

	if _, err := f.Write(buf); err != nil {
		return err
	}

	row := make([]byte, dims*4)
	for i, v := range vecs {
		if len(v) != dims {
			return fmt.Errorf("vector %d has %d dimensions, expected %d", i, len(v), dims)
		}
		for j, x := range v {
			binary.LittleEndian.PutUint32(row[j*4:], math.Float32bits(x))
		}
		if _, err := f.Write(row); err != nil {
			return err
		}
	}

	return f.Close()
}

// ReadVectors reads a vector file written by WriteVectors.
func ReadVectors(path string) ([][]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(data) < vecHeader || string(data[:len(vecMagic)]) != vecMagic {
		return nil, fmt.Errorf("%s is not a coroner vector file", path)
	}

	dims := int(binary.LittleEndian.Uint32(data[len(vecMagic):]))
	count := int(binary.LittleEndian.Uint32(data[len(vecMagic)+4:]))

	if dims < 0 || dims > maxVecDims {
		return nil, fmt.Errorf("%s claims %d dimensions, which is not plausible", path, dims)
	}

	want := vecHeader + count*dims*4
	if len(data) != want {
		return nil, fmt.Errorf("%s is %d bytes, expected %d for %d vectors of %d dimensions: the file is truncated or was written by a different version", path, len(data), want, count, dims)
	}

	out := make([][]float32, count)
	// One backing array for the whole file. At ten thousand documents this is a
	// few tens of megabytes, and slicing into it avoids that many small
	// allocations on every search.
	flat := make([]float32, count*dims)
	for i := range flat {
		flat[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[vecHeader+i*4:]))
	}
	for i := range out {
		out[i] = flat[i*dims : (i+1)*dims : (i+1)*dims]
	}

	return out, nil
}

// VectorDims reads just the width from a vector file, without loading it.
func VectorDims(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buf := make([]byte, vecHeader)
	if _, err := io.ReadFull(f, buf); err != nil {
		return 0, err
	}
	if string(buf[:len(vecMagic)]) != vecMagic {
		return 0, fmt.Errorf("%s is not a coroner vector file", path)
	}

	return int(binary.LittleEndian.Uint32(buf[len(vecMagic):])), nil
}

// Normalise scales a vector to unit length in place, so that cosine similarity
// is a plain dot product at search time. Doing it once at digest time rather
// than on every query is worth it; a zero vector is left alone, since it has no
// direction to preserve.
func Normalise(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}

	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}
