package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func writeDistillFixture(t *testing.T, rows int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "samples.csv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	if err := w.Write([]string{"success", "failure", "is_udp", "is_tcp", "weight", "teacher_weight"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		if err := w.Write([]string{
			strconv.Itoa(10 + i%7),
			strconv.Itoa(i % 2),
			"0",
			"1",
			strconv.FormatFloat(0.6+float64(i%10)*0.01, 'f', 4, 64),
			strconv.FormatFloat(0.7+float64(i%10)*0.01, 'f', 4, 64),
		}); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadSamplesUsesBoundedReservoir(t *testing.T) {
	path := writeDistillFixture(t, maxDistillSamplesPerBucket*3)
	got, err := loadSamples(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxDistillSamplesPerBucket {
		t.Fatalf("single-bucket reservoir=%d want=%d", len(got), maxDistillSamplesPerBucket)
	}

	again, err := loadSamples(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(got) {
		t.Fatalf("non-deterministic reservoir size: %d vs %d", len(got), len(again))
	}
	for i := range got {
		if got[i].TeacherWeight != again[i].TeacherWeight || got[i].ActualWeight != again[i].ActualWeight {
			t.Fatalf("reservoir is not reproducible at %d", i)
		}
	}
}

func TestLoadSamplesRejectsOversizeInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.csv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxDistillInputBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSamples(path); err == nil {
		t.Fatal("oversize distillation input was accepted")
	}
}
