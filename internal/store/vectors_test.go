package store

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestVectorRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.vec")

	want := [][]float32{
		{1, 2, 3},
		{-0.5, 0.25, 0},
		{0, 0, 0},
	}

	if err := WriteVectors(path, want); err != nil {
		t.Fatal(err)
	}

	got, err := ReadVectors(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != len(want) {
		t.Fatalf("read %d vectors, wrote %d", len(got), len(want))
	}
	for i := range want {
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Errorf("vector %d element %d: got %v, want %v", i, j, got[i][j], want[i][j])
			}
		}
	}
}

func TestVectorEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.vec")

	if err := WriteVectors(path, nil); err != nil {
		t.Fatal(err)
	}

	got, err := ReadVectors(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("an empty vector file read back %d vectors", len(got))
	}
}

func TestVectorRejectsRaggedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.vec")

	err := WriteVectors(path, [][]float32{{1, 2, 3}, {1, 2}})
	if err == nil {
		t.Error("writing vectors of different widths should fail rather than produce an unreadable file")
	}
}

// TestVectorRejectsTruncation guards the one failure mode this format has: the
// file carries no per-record framing, so a short read must be caught by the
// length check or it would silently return garbage vectors.
func TestVectorRejectsTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.vec")

	if err := WriteVectors(path, [][]float32{{1, 2, 3}, {4, 5, 6}}); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, data[:len(data)-4], 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadVectors(path); err == nil {
		t.Error("a truncated vector file was read without complaint")
	}
}

func TestVectorRejectsForeignFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.vec")
	if err := os.WriteFile(path, []byte("this is not a vector file at all"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadVectors(path); err == nil {
		t.Error("a file without the magic header was accepted")
	}
}

func TestNormaliseGivesUnitLength(t *testing.T) {
	v := []float32{3, 4}
	Normalise(v)

	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}

	if math.Abs(sum-1) > 1e-6 {
		t.Errorf("normalised vector has squared length %v, want 1", sum)
	}
}

func TestNormaliseLeavesZeroAlone(t *testing.T) {
	v := []float32{0, 0, 0}
	Normalise(v)

	for i, x := range v {
		if x != 0 {
			t.Errorf("element %d became %v; a zero vector has no direction to preserve", i, x)
		}
	}
}
