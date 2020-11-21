package prealloc

import (
	"go/parser"
	"go/token"
	"testing"
)

func Test_checkForPreallocations(t *testing.T) {
	const filename = "testdata/sample.go"

	trueVal := true
	got, err := checkForPreallocations([]string{filename}, &trueVal, &trueVal, &trueVal)
	if err != nil {
		t.Fatal(err)
	}

	want := []struct{
		Hint
		LineNumber int
	}{
		{
			LineNumber:        5,
			Hint: Hint{DeclaredSliceName: "y"},
		},
		{
			LineNumber:        6,
			Hint: Hint{DeclaredSliceName: "z"},
		},
		{
			LineNumber:        7,
			Hint: Hint{DeclaredSliceName: "t"},
		},
	}

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, filename, nil, 0); err != nil {
		t.Fatalf("could not parse file %q: %s", filename, err)
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d hints, but got %d: %+v", len(want), len(got), got)
	}

	for i := range got {
		act, exp := got[i], want[i]
		actFilename := fset.Position(act.Pos).Filename
		actLineNumber := fset.Position(act.Pos).Line

		if actFilename != filename {
			t.Errorf("wrong hints[%d].Filename: %q (expected: %q)", i, actFilename, filename)
		}

		if actLineNumber != exp.LineNumber {
			t.Errorf("wrong hints[%d].LineNumber: %d (expected: %d)", i, actLineNumber, exp.LineNumber)
		}

		if act.DeclaredSliceName != exp.DeclaredSliceName {
			t.Errorf("wrong hints[%d].DeclaredSliceName: %q (expected: %q)", i, act.DeclaredSliceName, exp.DeclaredSliceName)
		}
	}
}

func BenchmarkSize10NoPreallocate(b *testing.B) {
	existing := make([]int64, 10, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Don't preallocate our initial slice
		var init []int64
		for _, element := range existing {
			init = append(init, element)
		}
	}
}

func BenchmarkSize10Preallocate(b *testing.B) {
	existing := make([]int64, 10, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Preallocate our initial slice
		init := make([]int64, 0, len(existing))
		for _, element := range existing {
			init = append(init, element)
		}
	}
}

func BenchmarkSize10PreallocateCopy(b *testing.B) {
	existing := make([]int64, 10, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Preallocate our initial slice
		init := make([]int64, len(existing))
		copy(init, existing)
	}
}

func BenchmarkSize200NoPreallocate(b *testing.B) {
	existing := make([]int64, 200, 200)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Don't preallocate our initial slice
		var init []int64
		for _, element := range existing {
			init = append(init, element)
		}
	}
}

func BenchmarkSize200Preallocate(b *testing.B) {
	existing := make([]int64, 200, 200)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Preallocate our initial slice
		init := make([]int64, 0, len(existing))
		for _, element := range existing {
			init = append(init, element)
		}
	}
}

func BenchmarkSize200PreallocateCopy(b *testing.B) {
	existing := make([]int64, 200, 200)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Preallocate our initial slice
		init := make([]int64, len(existing))
		copy(init, existing)
	}
}
